package gc

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"warpstash/internal/database"
	"warpstash/internal/storage"
)

// CleanupTask represents a file scheduled for immediate or deferred unlinking.
type CleanupTask struct {
	ID          string
	StoragePath string
	SizeBytes   int64
	HardDelete  bool
}

// Cleaner coordinates the periodic GC of expired files and concurrent async deletion pipelines.
type Cleaner struct {
	db          *database.DB
	storage     storage.StorageEngine
	interval    time.Duration
	taskChan    chan CleanupTask
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
	purgedFiles atomic.Uint64
	purgedBytes atomic.Uint64
}

// NewCleaner creates a Cleaner instance with a dedicated buffered task channel.
func NewCleaner(db *database.DB, store storage.StorageEngine, interval time.Duration, workerCount int) *Cleaner {
	if workerCount <= 0 {
		workerCount = 4
	}
	if interval <= 0 {
		interval = 60 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := &Cleaner{
		db:       db,
		storage:  store,
		interval: interval,
		taskChan: make(chan CleanupTask, 1024),
		ctx:      ctx,
		cancel:   cancel,
	}

	// Start worker pool for physical disk unlinks
	for i := 0; i < workerCount; i++ {
		c.wg.Add(1)
		go c.workerLoop(i)
	}

	// Start periodic expiration ticker
	c.wg.Add(1)
	go c.tickerLoop()

	return c
}

// EnqueueImmediate unlinks a file asynchronously without blocking the calling HTTP request.
func (c *Cleaner) EnqueueImmediate(id, storagePath string, sizeBytes int64) {
	select {
	case <-c.ctx.Done():
		return
	default:
	}

	select {
	case c.taskChan <- CleanupTask{ID: id, StoragePath: storagePath, SizeBytes: sizeBytes}:
	default:
		// If channel is unexpectedly full, run in a separate detached goroutine
		go func() {
			c.processDelete(CleanupTask{ID: id, StoragePath: storagePath, SizeBytes: sizeBytes})
		}()
	}
}

// workerLoop processes individual file unlink tasks from the channel.
func (c *Cleaner) workerLoop(_ int) {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			// Drain remaining tasks before shutting down
			for {
				select {
				case task, ok := <-c.taskChan:
					if !ok {
						return
					}
					c.processDelete(task)
				default:
					return
				}
			}
		case task, ok := <-c.taskChan:
			if !ok {
				return
			}
			c.processDelete(task)
		}
	}
}

func (c *Cleaner) processDelete(task CleanupTask) {
	// 1. Unlink from disk
	if task.StoragePath != "" {
		if err := c.storage.Delete(task.StoragePath); err != nil {
			log.Printf("[GC] Warning: error deleting storage file %s: %v", task.StoragePath, err)
		}
	}

	// 2. Metadata handling:
	// If task.HardDelete is true (expired file), wipe from DB completely.
	// If false (immediate burn unlink), clear storage path and preserve tombstone so IsBurnedFile returns 410 Gone.
	if task.ID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if task.HardDelete {
			if err := c.db.HardDeleteFile(ctx, task.ID); err != nil {
				log.Printf("[GC] Error hard deleting file record %s: %v", task.ID, err)
			}
		} else {
			if err := c.db.ClearStoragePath(ctx, task.ID); err != nil {
				log.Printf("[GC] Error clearing storage path for %s: %v", task.ID, err)
			}
		}
		cancel()
	}

	c.purgedFiles.Add(1)
	if task.SizeBytes > 0 {
		c.purgedBytes.Add(uint64(task.SizeBytes))
	}
}

// tickerLoop periodically sweeps the database for expired files.
func (c *Cleaner) tickerLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.SweepOnce()
		}
	}
}

// SweepOnce runs a single pass of checking and purging expired or soft-deleted files.
func (c *Cleaner) SweepOnce() {
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()

	for {
		records, err := c.db.GetExpiredFiles(ctx, 100)
		if err != nil {
			log.Printf("[GC] Error querying expired files: %v", err)
			return
		}
		if len(records) == 0 {
			break
		}

		for _, rec := range records {
			// Hard delete only if the file's natural expiration time has passed
			isExpired := !rec.ExpiresAt.IsZero() && !rec.ExpiresAt.After(time.Now().UTC())
			c.processDelete(CleanupTask{
				ID:          rec.ID,
				StoragePath: rec.StoragePath,
				SizeBytes:   rec.SizeBytes,
				HardDelete:  isExpired,
			})
		}

		// If fewer than limit returned, no more expired files in this cycle
		if len(records) < 100 {
			break
		}
	}
}

// PurgedCount returns the total number of files permanently purged.
func (c *Cleaner) PurgedCount() uint64 {
	return c.purgedFiles.Load()
}

// PurgedBytes returns the total bytes reclaimed.
func (c *Cleaner) PurgedBytes() uint64 {
	return c.purgedBytes.Load()
}

// Stop gracefully terminates the GC workers and flushes remaining queue tasks.
func (c *Cleaner) Stop() {
	c.cancel()
	close(c.taskChan)
	c.wg.Wait()
}
