package gc

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"warpstash/internal/database"
	"warpstash/internal/storage"
)

func TestCleanerAsyncImmediate(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := database.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("db open failed: %v", err)
	}
	defer db.Close()

	store, err := storage.NewDiskStorage(filepath.Join(tmpDir, "storage"))
	if err != nil {
		t.Fatalf("storage init failed: %v", err)
	}

	ctx := context.Background()
	payload := []byte("temporary burn file content")
	relPath, size, hash, err := store.SaveStream(ctx, "burn1", bytes.NewReader(payload), 1024)
	if err != nil {
		t.Fatalf("save stream failed: %v", err)
	}

	record := &database.FileRecord{
		ID:           "burn1",
		OriginalName: "burn.txt",
		Extension:    ".txt",
		SizeBytes:    size,
		MimeType:     "text/plain",
		SHA256Hash:   hash,
		StoragePath:  relPath,
		DeleteToken:  "deltoken1",
		UploaderIP:   "127.0.0.1",
		IsBurnOnRead: true,
		CreatedAt:    time.Now().UTC(),
		ExpiresAt:    time.Now().UTC().Add(1 * time.Hour),
	}
	if err := db.InsertFile(ctx, record); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	cleaner := NewCleaner(db, store, 10*time.Second, 2)
	defer cleaner.Stop()

	// Enqueue immediate async unlink
	cleaner.EnqueueImmediate("burn1", relPath, size)

	// Wait briefly for worker to process
	time.Sleep(100 * time.Millisecond)

	// Verify file is gone from disk
	if store.Exists(relPath) {
		t.Errorf("file should have been unlinked from disk")
	}

	// Verify file is purged from DB
	_, err = db.GetFile(ctx, "burn1")
	if err == nil {
		t.Errorf("expected DB record to be purged")
	}

	if cleaner.PurgedCount() != 1 {
		t.Errorf("expected 1 purged file, got %d", cleaner.PurgedCount())
	}
}

func TestCleanerSweepExpired(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := database.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("db open failed: %v", err)
	}
	defer db.Close()

	store, err := storage.NewDiskStorage(filepath.Join(tmpDir, "storage"))
	if err != nil {
		t.Fatalf("storage init failed: %v", err)
	}

	ctx := context.Background()
	payload := []byte("already expired file")
	relPath, size, hash, err := store.SaveStream(ctx, "exp1", bytes.NewReader(payload), 1024)
	if err != nil {
		t.Fatalf("save stream failed: %v", err)
	}

	past := time.Now().UTC().Add(-10 * time.Minute)
	record := &database.FileRecord{
		ID:           "exp1",
		OriginalName: "expired.txt",
		Extension:    ".txt",
		SizeBytes:    size,
		MimeType:     "text/plain",
		SHA256Hash:   hash,
		StoragePath:  relPath,
		DeleteToken:  "deltokenexp",
		UploaderIP:   "127.0.0.1",
		IsBurnOnRead: false,
		CreatedAt:    past.Add(-10 * time.Minute),
		ExpiresAt:    past, // expired!
	}
	if err := db.InsertFile(ctx, record); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	cleaner := NewCleaner(db, store, 10*time.Second, 2)
	defer cleaner.Stop()

	// Trigger manual sweep
	cleaner.SweepOnce()

	if store.Exists(relPath) {
		t.Errorf("expired file should have been deleted from disk")
	}

	_, err = db.GetFile(ctx, "exp1")
	if err == nil {
		t.Errorf("expired file should have been purged from DB")
	}
}
