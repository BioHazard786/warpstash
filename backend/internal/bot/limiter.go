package bot

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type userLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// UserRateLimiter manages in-memory token-bucket rate limiters keyed by Telegram user ID.
type UserRateLimiter struct {
	mu       sync.RWMutex
	limiters map[int64]*userLimiter
	rate     rate.Limit
	burst    int
	stopChan chan struct{}
}

// NewUserRateLimiter creates a UserRateLimiter and launches a periodic background eviction routine.
// If r <= 0 or burst <= 0, rate limiting is disabled and Allow() will always return true.
func NewUserRateLimiter(r rate.Limit, burst int) *UserRateLimiter {
	lim := &UserRateLimiter{
		limiters: make(map[int64]*userLimiter),
		rate:     r,
		burst:    burst,
		stopChan: make(chan struct{}),
	}

	if r > 0 && burst > 0 {
		go lim.cleanupStale(10 * time.Minute)
	}

	return lim
}

// Allow reports whether an event may happen now for the given Telegram user ID.
func (u *UserRateLimiter) Allow(userID int64) bool {
	if u == nil || u.rate <= 0 || u.burst <= 0 {
		return true
	}

	u.mu.RLock()
	client, exists := u.limiters[userID]
	if exists {
		client.lastSeen = time.Now()
		lim := client.limiter
		u.mu.RUnlock()
		return lim.Allow()
	}
	u.mu.RUnlock()

	u.mu.Lock()
	defer u.mu.Unlock()

	client, exists = u.limiters[userID]
	if !exists {
		client = &userLimiter{
			limiter:  rate.NewLimiter(u.rate, u.burst),
			lastSeen: time.Now(),
		}
		u.limiters[userID] = client
	} else {
		client.lastSeen = time.Now()
	}

	return client.limiter.Allow()
}

// Stop terminates the background eviction ticker routine.
func (u *UserRateLimiter) Stop() {
	if u == nil {
		return
	}
	select {
	case <-u.stopChan:
		return
	default:
		close(u.stopChan)
	}
}

func (u *UserRateLimiter) cleanupStale(idleTimeout time.Duration) {
	ticker := time.NewTicker(idleTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-u.stopChan:
			return
		case <-ticker.C:
			u.mu.Lock()
			now := time.Now()
			for id, client := range u.limiters {
				if now.Sub(client.lastSeen) > idleTimeout {
					delete(u.limiters, id)
				}
			}
			u.mu.Unlock()
		}
	}
}
