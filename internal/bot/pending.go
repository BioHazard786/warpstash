package bot

import (
	"sync"
	"time"

	"github.com/gotd/td/tg"
)

// PendingUpload stores metadata for an uploaded media message awaiting expiration selection.
type PendingUpload struct {
	Media     tg.MessageMediaClass
	Filename  string
	Size      int64
	MimeType  string
	UserID    int64
	InputPeer tg.InputPeerClass
	CreatedAt time.Time
}

// PendingManager provides a thread-safe, self-expiring store for pending uploads.
type PendingManager struct {
	mu    sync.RWMutex
	items map[string]*PendingUpload
	ttl   time.Duration
}

// NewPendingManager initializes a PendingManager and starts a background eviction loop.
func NewPendingManager(ttl time.Duration) *PendingManager {
	pm := &PendingManager{
		items: make(map[string]*PendingUpload),
		ttl:   ttl,
	}
	go pm.cleanupLoop()
	return pm
}

// Store records a pending upload by key.
func (pm *PendingManager) Store(key string, p *PendingUpload) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.items[key] = p
}

// Get retrieves a pending upload by key, verifying it hasn't expired.
func (pm *PendingManager) Get(key string) (*PendingUpload, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	p, ok := pm.items[key]
	if !ok || time.Since(p.CreatedAt) > pm.ttl {
		return nil, false
	}
	return p, true
}

// Delete removes a pending upload from memory.
func (pm *PendingManager) Delete(key string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.items, key)
}

func (pm *PendingManager) cleanupLoop() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		pm.mu.Lock()
		now := time.Now()
		for k, v := range pm.items {
			if now.Sub(v.CreatedAt) > pm.ttl {
				delete(pm.items, k)
			}
		}
		pm.mu.Unlock()
	}
}
