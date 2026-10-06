package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps the standard *sql.DB instance with helper query methods.
type DB struct {
	*sql.DB
	usageMu        sync.RWMutex
	cachedUsage    int64
	usageCacheTime time.Time
}

// FileRecord represents the metadata stored for an uploaded file.
type FileRecord struct {
	ID            string     `json:"id"`
	OriginalName  string     `json:"original_name"`
	Extension     string     `json:"extension"`
	SizeBytes     int64      `json:"size_bytes"`
	MimeType      string     `json:"mime_type"`
	SHA256Hash    string     `json:"sha256_hash"`
	StoragePath   string     `json:"storage_path"`
	DeleteToken   string     `json:"delete_token"`
	UploaderIP    string     `json:"uploader_ip"`
	IsBurnOnRead  bool       `json:"is_burn_on_read"`
	DownloadCount int64      `json:"download_count"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiresAt     time.Time  `json:"expires_at"`
	DeletedAt     *time.Time `json:"deleted_at,omitempty"`
}

// Open initializes the SQLite database at dbPath, enabling WAL mode and running migrations.
func Open(dbPath string) (*DB, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory %s: %w", dir, err)
	}

	// modernc.org/sqlite DSN with pragmas
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Configure connection pool for SQLite
	// MaxOpenConns can be > 1 in WAL mode for readers, but 1 writer at a time is managed by SQLite locking
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	sdb := &DB{DB: db}
	if err := sdb.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	return sdb, nil
}

// migrate creates tables and indexes if they do not exist.
func (db *DB) migrate(ctx context.Context) error {
	schema := `
	CREATE TABLE IF NOT EXISTS files (
		id TEXT PRIMARY KEY,
		original_name TEXT NOT NULL,
		extension TEXT NOT NULL,
		size_bytes INTEGER NOT NULL,
		mime_type TEXT NOT NULL,
		sha256_hash TEXT NOT NULL,
		storage_path TEXT NOT NULL,
		delete_token TEXT NOT NULL UNIQUE,
		uploader_ip TEXT NOT NULL,
		is_burn_on_read BOOLEAN NOT NULL DEFAULT 0,
		download_count INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		expires_at DATETIME NOT NULL,
		deleted_at DATETIME
	);

	CREATE INDEX IF NOT EXISTS idx_files_expires_at ON files (expires_at);
	CREATE INDEX IF NOT EXISTS idx_files_hash ON files (sha256_hash);
	CREATE INDEX IF NOT EXISTS idx_files_storage_cleanup ON files (storage_path) WHERE deleted_at IS NOT NULL AND storage_path != '';
	`
	_, err := db.ExecContext(ctx, schema)
	return err
}

// InsertFile stores file metadata in the database.
func (db *DB) InsertFile(ctx context.Context, f *FileRecord) error {
	query := `
	INSERT INTO files (
		id, original_name, extension, size_bytes, mime_type, sha256_hash,
		storage_path, delete_token, uploader_ip, is_burn_on_read, download_count,
		created_at, expires_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.ExecContext(ctx, query,
		f.ID,
		f.OriginalName,
		f.Extension,
		f.SizeBytes,
		f.MimeType,
		f.SHA256Hash,
		f.StoragePath,
		f.DeleteToken,
		f.UploaderIP,
		f.IsBurnOnRead,
		f.DownloadCount,
		f.CreatedAt.UTC(),
		f.ExpiresAt.UTC(),
	)
	if err == nil {
		db.invalidateUsageCache()
	}
	return err
}

// GetFile retrieves an active file by its ID.
func (db *DB) GetFile(ctx context.Context, id string) (*FileRecord, error) {
	query := `
	SELECT id, original_name, extension, size_bytes, mime_type, sha256_hash,
	       storage_path, delete_token, uploader_ip, is_burn_on_read, download_count,
	       created_at, expires_at, deleted_at
	FROM files
	WHERE id = ? AND deleted_at IS NULL
	`
	row := db.QueryRowContext(ctx, query, id)
	return scanFileRecord(row)
}

// GetFileByDeleteToken finds a file by its unique deletion secret token.
func (db *DB) GetFileByDeleteToken(ctx context.Context, token string) (*FileRecord, error) {
	query := `
	SELECT id, original_name, extension, size_bytes, mime_type, sha256_hash,
	       storage_path, delete_token, uploader_ip, is_burn_on_read, download_count,
	       created_at, expires_at, deleted_at
	FROM files
	WHERE delete_token = ? AND deleted_at IS NULL
	`
	row := db.QueryRowContext(ctx, query, token)
	return scanFileRecord(row)
}

// AtomicClaimBurnFile performs a race-condition-proof claim on a one-time burn-after-reading file.
// If the file is burn-on-read and has never been downloaded, it atomically marks it as downloaded
// and sets deleted_at to current timestamp, returning the file record.
// If it was already downloaded or does not exist, returns sql.ErrNoRows.
func (db *DB) AtomicClaimBurnFile(ctx context.Context, id string) (*FileRecord, error) {
	query := `
	UPDATE files
	SET download_count = download_count + 1,
	    deleted_at = CURRENT_TIMESTAMP
	WHERE id = ?
	  AND is_burn_on_read = 1
	  AND download_count = 0
	  AND deleted_at IS NULL
	RETURNING id, original_name, extension, size_bytes, mime_type, sha256_hash,
	          storage_path, delete_token, uploader_ip, is_burn_on_read, download_count,
	          created_at, expires_at, deleted_at
	`
	row := db.QueryRowContext(ctx, query, id)
	return scanFileRecord(row)
}

// IsBurnedFile checks if a file with the given ID existed as a burn-on-read file and has been downloaded or deleted.
func (db *DB) IsBurnedFile(ctx context.Context, id string) (bool, error) {
	query := `SELECT 1 FROM files WHERE id = ? AND is_burn_on_read = 1 AND (deleted_at IS NOT NULL OR download_count > 0)`
	var exists int
	err := db.QueryRowContext(ctx, query, id).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// IncrementDownloadCount increments the download counter for a non-burn file.
func (db *DB) IncrementDownloadCount(ctx context.Context, id string) error {
	query := `UPDATE files SET download_count = download_count + 1 WHERE id = ? AND deleted_at IS NULL`
	_, err := db.ExecContext(ctx, query, id)
	return err
}

// SoftDeleteByToken marks a file as deleted using its delete token and returns its storage path.
func (db *DB) SoftDeleteByToken(ctx context.Context, token string) (*FileRecord, error) {
	query := `
	UPDATE files
	SET deleted_at = CURRENT_TIMESTAMP
	WHERE delete_token = ? AND deleted_at IS NULL
	RETURNING id, original_name, extension, size_bytes, mime_type, sha256_hash,
	          storage_path, delete_token, uploader_ip, is_burn_on_read, download_count,
	          created_at, expires_at, deleted_at
	`
	row := db.QueryRowContext(ctx, query, token)
	return scanFileRecord(row)
}

// SoftDeleteByID marks a file as deleted by ID.
func (db *DB) SoftDeleteByID(ctx context.Context, id string) error {
	query := `UPDATE files SET deleted_at = CURRENT_TIMESTAMP WHERE id = ? AND deleted_at IS NULL`
	res, err := db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetExpiredFiles returns a batch of files ready to be physically purged by the GC worker.
func (db *DB) GetExpiredFiles(ctx context.Context, limit int) ([]*FileRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `
	SELECT id, original_name, extension, size_bytes, mime_type, sha256_hash,
	       storage_path, delete_token, uploader_ip, is_burn_on_read, download_count,
	       created_at, expires_at, deleted_at
	FROM files
	WHERE expires_at <= CURRENT_TIMESTAMP
	   OR (deleted_at IS NOT NULL AND storage_path != '')
	LIMIT ?
	`
	rows, err := db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*FileRecord
	for rows.Next() {
		f := &FileRecord{}
		var deletedAt sql.NullTime
		var createdAt, expiresAt time.Time

		err := rows.Scan(
			&f.ID,
			&f.OriginalName,
			&f.Extension,
			&f.SizeBytes,
			&f.MimeType,
			&f.SHA256Hash,
			&f.StoragePath,
			&f.DeleteToken,
			&f.UploaderIP,
			&f.IsBurnOnRead,
			&f.DownloadCount,
			&createdAt,
			&expiresAt,
			&deletedAt,
		)
		if err != nil {
			return nil, err
		}
		f.CreatedAt = createdAt
		f.ExpiresAt = expiresAt
		if deletedAt.Valid {
			f.DeletedAt = &deletedAt.Time
		}
		records = append(records, f)
	}

	return records, rows.Err()
}

func (db *DB) invalidateUsageCache() {
	db.usageMu.Lock()
	db.usageCacheTime = time.Time{}
	db.usageMu.Unlock()
}

// HardDeleteFile permanently deletes the file record from the database.
func (db *DB) HardDeleteFile(ctx context.Context, id string) error {
	query := `DELETE FROM files WHERE id = ?`
	_, err := db.ExecContext(ctx, query, id)
	if err == nil {
		db.invalidateUsageCache()
	}
	return err
}

// ClearStoragePath clears the on-disk storage path and ensures deleted_at is set, preserving the metadata tombstone.
func (db *DB) ClearStoragePath(ctx context.Context, id string) error {
	query := `UPDATE files SET storage_path = '', deleted_at = COALESCE(deleted_at, CURRENT_TIMESTAMP) WHERE id = ?`
	_, err := db.ExecContext(ctx, query, id)
	if err == nil {
		db.invalidateUsageCache()
	}
	return err
}

// GetTotalStorageUsage calculates the sum of all active files in bytes with a 10-second TTL cache.
func (db *DB) GetTotalStorageUsage(ctx context.Context) (int64, error) {
	db.usageMu.RLock()
	if time.Since(db.usageCacheTime) < 10*time.Second {
		val := db.cachedUsage
		db.usageMu.RUnlock()
		return val, nil
	}
	db.usageMu.RUnlock()

	db.usageMu.Lock()
	defer db.usageMu.Unlock()

	if time.Since(db.usageCacheTime) < 10*time.Second {
		return db.cachedUsage, nil
	}

	query := `SELECT COALESCE(SUM(size_bytes), 0) FROM files WHERE deleted_at IS NULL`
	var total int64
	err := db.QueryRowContext(ctx, query).Scan(&total)
	if err == nil {
		db.cachedUsage = total
		db.usageCacheTime = time.Now()
	}
	return total, err
}

// Helper scanner
type scannable interface {
	Scan(dest ...any) error
}

func scanFileRecord(s scannable) (*FileRecord, error) {
	f := &FileRecord{}
	var deletedAt sql.NullTime
	var createdAt, expiresAt time.Time

	err := s.Scan(
		&f.ID,
		&f.OriginalName,
		&f.Extension,
		&f.SizeBytes,
		&f.MimeType,
		&f.SHA256Hash,
		&f.StoragePath,
		&f.DeleteToken,
		&f.UploaderIP,
		&f.IsBurnOnRead,
		&f.DownloadCount,
		&createdAt,
		&expiresAt,
		&deletedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}

	f.CreatedAt = createdAt
	f.ExpiresAt = expiresAt
	if deletedAt.Valid {
		f.DeletedAt = &deletedAt.Time
	}

	return f, nil
}
