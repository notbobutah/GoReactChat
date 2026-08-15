package orchestrator

import (
	"sync"
	"time"
)

// Decision is the rate limiter's answer for one attempted model call.
type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
}

// RateLimiter gates model calls per (user × workspace × tier).
type RateLimiter interface {
	Check(key RateKey) Decision
}

// RateKey identifies one rate-limit bucket.
type RateKey struct {
	UserID      string
	WorkspaceID string
	Tier        Tier
}

// WindowRateLimiter is a fixed-window in-process limiter. Fine for a single
// instance; a Redis-backed limiter replaces it when we scale horizontally
// (same interface, so nothing above this line changes).
type WindowRateLimiter struct {
	limit  int
	window time.Duration

	mu      sync.Mutex
	buckets map[RateKey]*window
	now     func() time.Time // injectable for tests
}

type window struct {
	start time.Time
	count int
}

func NewWindowRateLimiter(limit int, w time.Duration) *WindowRateLimiter {
	return &WindowRateLimiter{
		limit:   limit,
		window:  w,
		buckets: make(map[RateKey]*window),
		now:     time.Now,
	}
}

func (r *WindowRateLimiter) Check(key RateKey) Decision {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	b, ok := r.buckets[key]
	if !ok || now.Sub(b.start) >= r.window {
		r.buckets[key] = &window{start: now, count: 1}
		return Decision{Allowed: true}
	}

	if b.count >= r.limit {
		return Decision{Allowed: false, RetryAfter: r.window - now.Sub(b.start)}
	}

	b.count++
	return Decision{Allowed: true}
}
