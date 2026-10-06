package api

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"warpstash/internal/config"
)

// IPRateLimiter manages per-IP rate limiters with automatic eviction of stale entries.
type IPRateLimiter struct {
	mu       sync.RWMutex
	limiters map[string]*clientLimiter
	rate     rate.Limit
	burst    int
	stopChan chan struct{}
}

type clientLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewIPRateLimiter creates an IPRateLimiter and starts a periodic cleanup routine.
func NewIPRateLimiter(r rate.Limit, burst int) *IPRateLimiter {
	lim := &IPRateLimiter{
		limiters: make(map[string]*clientLimiter),
		rate:     r,
		burst:    burst,
		stopChan: make(chan struct{}),
	}

	go lim.cleanupStale(5 * time.Minute)
	return lim
}

// Stop cleanly terminates the rate limiter's background eviction routine.
func (i *IPRateLimiter) Stop() {
	select {
	case <-i.stopChan:
		return
	default:
		close(i.stopChan)
	}
}

func (i *IPRateLimiter) getLimiter(ip string) *rate.Limiter {
	i.mu.RLock()
	client, exists := i.limiters[ip]
	if exists {
		client.lastSeen = time.Now()
		lim := client.limiter
		i.mu.RUnlock()
		return lim
	}
	i.mu.RUnlock()

	i.mu.Lock()
	defer i.mu.Unlock()

	client, exists = i.limiters[ip]
	if !exists {
		client = &clientLimiter{
			limiter:  rate.NewLimiter(i.rate, i.burst),
			lastSeen: time.Now(),
		}
		i.limiters[ip] = client
	} else {
		client.lastSeen = time.Now()
	}

	return client.limiter
}

func (i *IPRateLimiter) cleanupStale(idleTimeout time.Duration) {
	ticker := time.NewTicker(idleTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-i.stopChan:
			return
		case <-ticker.C:
			i.mu.Lock()
			now := time.Now()
			for ip, client := range i.limiters {
				if now.Sub(client.lastSeen) > idleTimeout {
					delete(i.limiters, ip)
				}
			}
			i.mu.Unlock()
		}
	}
}

// RateLimitMiddleware enforces token bucket rate limiting per client IP.
func RateLimitMiddleware(limiter *IPRateLimiter, cfg *config.Config) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Exempt static web assets from client rate limit quotas
			if strings.HasPrefix(r.URL.Path, "/_astro/") || r.URL.Path == "/favicon.ico" || r.URL.Path == "/favicon.svg" {
				next.ServeHTTP(w, r)
				return
			}

			ip := ClientIP(r, cfg.TrustProxy)
			l := limiter.getLimiter(ip)
			if !l.Allow() {
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAuthTokenMiddleware enforces upload authorization if WARPSTASH_AUTH_TOKEN is configured.
// If AuthToken is empty, all requests pass through (open/public model).
func RequireAuthTokenMiddleware(cfg *config.Config) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.AuthToken == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Check Authorization header (Bearer <token>)
			authHeader := r.Header.Get("Authorization")
			token := ""
			if strings.HasPrefix(authHeader, "Bearer ") {
				token = strings.TrimPrefix(authHeader, "Bearer ")
			} else if h := r.Header.Get("X-Auth-Token"); h != "" {
				token = h
			} else if q := r.URL.Query().Get("auth_token"); q != "" {
				token = q
			}

			if subtle.ConstantTimeCompare([]byte(token), []byte(cfg.AuthToken)) != 1 {
				http.Error(w, "Unauthorized: invalid or missing auth token", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP safely determines the real client IP address.
// If trustProxy is false, it returns the socket peer IP from r.RemoteAddr,
// preventing X-Forwarded-For / X-Real-IP spoofing attacks.
// If trustProxy is true, it extracts the IP forwarded by reverse proxies.
func ClientIP(r *http.Request, trustProxy bool) string {
	peerIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peerIP = r.RemoteAddr
	}

	if !trustProxy {
		return peerIP
	}

	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		return xri
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	return peerIP
}
