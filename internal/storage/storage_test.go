package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
)

func TestStorageLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewDiskStorage(tmpDir)
	if err != nil {
		t.Fatalf("failed to create disk storage: %v", err)
	}

	payload := []byte("hello warpstash streaming storage test")
	hasher := sha256.New()
	hasher.Write(payload)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))

	ctx := context.Background()
	id := "ab12cd34"

	// 1. Save stream
	relPath, size, hash, err := store.SaveStream(ctx, id, bytes.NewReader(payload), 1024*1024)
	if err != nil {
		t.Fatalf("failed to save stream: %v", err)
	}

	if size != int64(len(payload)) {
		t.Errorf("expected size %d, got %d", len(payload), size)
	}
	if hash != expectedHash {
		t.Errorf("expected hash %s, got %s", expectedHash, hash)
	}
	if !store.Exists(relPath) {
		t.Fatalf("expected file to exist at %s", relPath)
	}

	// 2. Open & read back
	rc, info, err := store.Open(relPath)
	if err != nil {
		t.Fatalf("failed to open stored file: %v", err)
	}
	defer rc.Close()

	if info.Size() != int64(len(payload)) {
		t.Errorf("expected stat size %d, got %d", len(payload), info.Size())
	}

	readBack, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("failed to read back stream: %v", err)
	}
	if !bytes.Equal(readBack, payload) {
		t.Errorf("read payload mismatch")
	}

	// 3. Test Delete
	if err := store.Delete(relPath); err != nil {
		t.Fatalf("failed to delete file: %v", err)
	}
	if store.Exists(relPath) {
		t.Errorf("file still exists after deletion")
	}
}

func TestStorageFileTooLarge(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewDiskStorage(tmpDir)
	if err != nil {
		t.Fatalf("failed to create disk storage: %v", err)
	}

	payload := []byte("abcdefghijklmnopqrstuvwxyz") // 26 bytes
	ctx := context.Background()

	// Limit to 10 bytes -> must return ErrFileTooLarge
	_, _, _, err = store.SaveStream(ctx, "largefile1", bytes.NewReader(payload), 10)
	if !errors.Is(err, ErrFileTooLarge) {
		t.Errorf("expected ErrFileTooLarge, got %v", err)
	}

	// Verify no partial file was left behind
	_, relFile := store.shardPath("largefile1")
	if store.Exists(relFile) {
		t.Errorf("expected temp file to be cleaned up on limit error")
	}
}

func TestStorageDiskSpace(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewDiskStorage(tmpDir)
	if err != nil {
		t.Fatalf("failed to create disk storage: %v", err)
	}

	space, err := store.DiskSpace()
	if err != nil {
		t.Fatalf("failed to query disk space: %v", err)
	}

	if space.TotalBytes == 0 {
		t.Errorf("expected non-zero total disk bytes")
	}
	if space.AvailableBytes == 0 {
		t.Errorf("expected non-zero available disk bytes")
	}

	hasSpace, err := store.HasAvailableDiskSpace(1024) // 1KB
	if err != nil {
		t.Fatalf("has space check failed: %v", err)
	}
	if !hasSpace {
		t.Logf("system does not have 1KB + safety reserve available")
	}
}

