// Package limit is a small fixed-window rate limiter keyed by string
// (deployment.md §9.3: login attempts per IP and per code, session creation
// per user, stuck reports per hour).
package limit

import (
	"sync"
	"time"
)

// Limiter allows N events per window per key.
type Limiter struct {
	n      int
	window time.Duration

	mu      sync.Mutex
	buckets map[string]*bucket
	sweep   time.Time
}

type bucket struct {
	start time.Time
	count int
}

// New returns a limiter allowing n events per window per key.
func New(n int, window time.Duration) *Limiter {
	return &Limiter{n: n, window: window, buckets: map[string]*bucket{}}
}

// Allow records an event for key and reports whether it is within the
// limit. A nil limiter allows everything.
func (l *Limiter) Allow(key string) bool {
	if l == nil || l.n <= 0 {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.sweep) > l.window {
		for k, b := range l.buckets {
			if now.Sub(b.start) >= l.window {
				delete(l.buckets, k)
			}
		}
		l.sweep = now
	}
	b := l.buckets[key]
	if b == nil || now.Sub(b.start) >= l.window {
		b = &bucket{start: now}
		l.buckets[key] = b
	}
	if b.count >= l.n {
		return false
	}
	b.count++
	return true
}
