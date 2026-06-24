package manager

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// rateLimiter implements a sliding-window rate limiter with per-key tracking.
type rateLimiter struct {
	mu      sync.Mutex
	entries map[string]*rateEntry
	limit   int
	window  time.Duration
}

type rateEntry struct {
	lastReset time.Time
	count     int
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		entries: make(map[string]*rateEntry),
		limit:   limit,
		window:  window,
	}
}

// Allow returns true if the request is allowed, false if rate-limited.
// Key is cleaned (lowercased, trimmed) for consistent tracking.
func (rl *rateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	key = strings.ToLower(strings.TrimSpace(key))

	e, ok := rl.entries[key]
	if !ok || now.Sub(e.lastReset) > rl.window {
		rl.entries[key] = &rateEntry{
			lastReset: now,
			count:     1,
		}
		return true
	}

	e.count++
	return e.count <= rl.limit
}

// RetryAfter returns the time until the next request would be allowed.
func (rl *rateLimiter) RetryAfter(key string) time.Duration {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	key = strings.ToLower(strings.TrimSpace(key))
	e, ok := rl.entries[key]
	if !ok {
		return 0
	}
	remaining := rl.window - time.Since(e.lastReset)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// perUserRateLimiter enforces per-user rate limits for authenticated operations.
type perUserRateLimiter struct {
	mu      sync.Mutex
	entries map[string]*rateEntry
	limit   int
	window  time.Duration
}

func newPerUserRateLimiter(limit int, window time.Duration) *perUserRateLimiter {
	return &perUserRateLimiter{
		entries: make(map[string]*rateEntry),
		limit:   limit,
		window:  window,
	}
}

func (rl *perUserRateLimiter) Allow(username string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	username = strings.ToLower(strings.TrimSpace(username))

	e, ok := rl.entries[username]
	if !ok || now.Sub(e.lastReset) > rl.window {
		rl.entries[username] = &rateEntry{
			lastReset: now,
			count:     1,
		}
		return true
	}

	e.count++
	return e.count <= rl.limit
}

// clientIP extracts the client IP from the request, respecting X-Forwarded-For.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		ip := strings.TrimSpace(parts[0])
		if ip != "" {
			return ip
		}
	}
	host, _, err := splitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// splitHostPort strips the port from a "host:port" string.
func splitHostPort(addr string) (string, string, error) {
	i := strings.LastIndex(addr, ":")
	if i == -1 {
		return addr, "", nil
	}
	return addr[:i], addr[i+1:], nil
}
