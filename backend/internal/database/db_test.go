package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func setupTestDB(t *testing.T) *DB {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_warpstash.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
	})
	return db
}

func TestDBFileLifecycle(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	file := &FileRecord{
		ID:           "test1234",
		OriginalName: "test.txt",
		Extension:    ".txt",
		SizeBytes:    1024,
		MimeType:     "text/plain",
		SHA256Hash:   "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		StoragePath:  "te/st/test1234.dat",
		DeleteToken:  "token1234567890abcdef1234567890abcdef",
		UploaderIP:   "127.0.0.1",
		IsBurnOnRead: false,
		CreatedAt:    now,
		ExpiresAt:    now.Add(24 * time.Hour),
	}

	// 1. Insert
	if err := db.InsertFile(ctx, file); err != nil {
		t.Fatalf("failed to insert file: %v", err)
	}

	// 2. Retrieve
	retrieved, err := db.GetFile(ctx, "test1234")
	if err != nil {
		t.Fatalf("failed to get file: %v", err)
	}
	if retrieved.OriginalName != file.OriginalName {
		t.Errorf("expected name %s, got %s", file.OriginalName, retrieved.OriginalName)
	}

	// 3. Increment download count
	if err := db.IncrementDownloadCount(ctx, "test1234"); err != nil {
		t.Fatalf("failed to increment download count: %v", err)
	}
	retrieved, _ = db.GetFile(ctx, "test1234")
	if retrieved.DownloadCount != 1 {
		t.Errorf("expected download count 1, got %d", retrieved.DownloadCount)
	}

	// 4. Soft delete by token
	deleted, err := db.SoftDeleteByToken(ctx, file.DeleteToken)
	if err != nil {
		t.Fatalf("failed to soft delete by token: %v", err)
	}
	if deleted.ID != file.ID {
		t.Errorf("expected deleted id %s, got %s", file.ID, deleted.ID)
	}

	// 5. Subsequent GetFile should return ErrNoRows
	_, err = db.GetFile(ctx, "test1234")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected ErrNoRows after deletion, got %v", err)
	}
}

func TestDBAtomicBurnOnRead(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	burnFile := &FileRecord{
		ID:           "burn1234",
		OriginalName: "secret.key",
		Extension:    ".key",
		SizeBytes:    256,
		MimeType:     "application/octet-stream",
		SHA256Hash:   "hash123",
		StoragePath:  "bu/rn/burn1234.dat",
		DeleteToken:  "token_burn",
		UploaderIP:   "127.0.0.1",
		IsBurnOnRead: true,
		CreatedAt:    now,
		ExpiresAt:    now.Add(1 * time.Hour),
	}

	if err := db.InsertFile(ctx, burnFile); err != nil {
		t.Fatalf("failed to insert burn file: %v", err)
	}

	// First claim: should succeed
	claimed, err := db.AtomicClaimBurnFile(ctx, "burn1234")
	if err != nil {
		t.Fatalf("first claim failed: %v", err)
	}
	if claimed.DownloadCount != 1 {
		t.Errorf("expected download count 1, got %d", claimed.DownloadCount)
	}
	if claimed.DeletedAt == nil {
		t.Errorf("expected deleted_at to be set")
	}

	// Second claim: must fail with sql.ErrNoRows (already consumed/burned!)
	_, err = db.AtomicClaimBurnFile(ctx, "burn1234")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected second claim to return ErrNoRows, got %v", err)
	}

	// GetFile must also fail (it's marked deleted)
	_, err = db.GetFile(ctx, "burn1234")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected GetFile to return ErrNoRows, got %v", err)
	}
}
