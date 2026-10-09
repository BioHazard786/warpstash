package bot

import (
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestUserRateLimiter_Allow(t *testing.T) {
	// 1 token/sec with burst of 2
	lim := NewUserRateLimiter(rate.Limit(1), 2)
	defer lim.Stop()

	userA := int64(1001)
	userB := int64(1002)

	// First 2 requests for User A should succeed (burst = 2)
	if !lim.Allow(userA) {
		t.Errorf("expected 1st request from userA to be allowed")
	}
	if !lim.Allow(userA) {
		t.Errorf("expected 2nd request from userA to be allowed")
	}

	// 3rd rapid request for User A should be rate limited
	if lim.Allow(userA) {
		t.Errorf("expected 3rd immediate request from userA to be blocked")
	}

	// User B should have independent bucket and succeed
	if !lim.Allow(userB) {
		t.Errorf("expected 1st request from userB to be allowed independently")
	}
	if !lim.Allow(userB) {
		t.Errorf("expected 2nd request from userB to be allowed independently")
	}
	if lim.Allow(userB) {
		t.Errorf("expected 3rd immediate request from userB to be blocked")
	}
}

func TestUserRateLimiter_Disabled(t *testing.T) {
	// Zero rate should disable limiting
	limZero := NewUserRateLimiter(rate.Limit(0), 0)
	defer limZero.Stop()

	for i := 0; i < 10; i++ {
		if !limZero.Allow(123) {
			t.Errorf("expected disabled limiter to allow all requests, failed on iteration %d", i)
		}
	}

	// Nil limiter should be safe and allow all requests
	var nilLim *UserRateLimiter
	if !nilLim.Allow(123) {
		t.Errorf("expected nil limiter to allow requests safely")
	}
}

func TestUserRateLimiter_Stop(t *testing.T) {
	lim := NewUserRateLimiter(rate.Limit(1), 2)
	lim.Stop()
	// Calling Stop multiple times should be safe
	lim.Stop()

	var nilLim *UserRateLimiter
	nilLim.Stop()
}

func TestUserRateLimiter_Cleanup(t *testing.T) {
	lim := NewUserRateLimiter(rate.Limit(10), 10)
	defer lim.Stop()

	user := int64(9999)
	lim.Allow(user)

	lim.mu.RLock()
	_, exists := lim.limiters[user]
	lim.mu.RUnlock()
	if !exists {
		t.Fatalf("expected user to exist in limiter map")
	}

	// Artificially age the entry
	lim.mu.Lock()
	lim.limiters[user].lastSeen = time.Now().Add(-15 * time.Minute)
	lim.mu.Unlock()

	// Trigger manual cleanup logic
	lim.mu.Lock()
	now := time.Now()
	for id, client := range lim.limiters {
		if now.Sub(client.lastSeen) > 10*time.Minute {
			delete(lim.limiters, id)
		}
	}
	lim.mu.Unlock()

	lim.mu.RLock()
	_, exists = lim.limiters[user]
	lim.mu.RUnlock()
	if exists {
		t.Errorf("expected stale user entry to be evicted")
	}
}
