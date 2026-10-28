package auth

import (
	"sync"
	"time"
)

// Limiter is an in-memory login throttle keyed by username and client address.
// A key that fails MaxFailures times within Window is locked for LockFor.
// This is per process, which matches the single-instance SQLite deployment.
type Limiter struct {
	mu          sync.Mutex
	now         func() time.Time
	maxFailures int
	window      time.Duration
	lockFor     time.Duration
	entries     map[string]*throttleEntry
	ops         int
}

type throttleEntry struct {
	failures    int
	windowStart time.Time
	lockedUntil time.Time
}

// NewLimiter creates a limiter. Zero values select 5 failures per 15 minutes, locked for 15 minutes.
func NewLimiter(now func() time.Time, maxFailures int, window, lockFor time.Duration) *Limiter {
	if now == nil {
		now = time.Now
	}
	if maxFailures <= 0 {
		maxFailures = 5
	}
	if window <= 0 {
		window = 15 * time.Minute
	}
	if lockFor <= 0 {
		lockFor = 15 * time.Minute
	}
	return &Limiter{
		now:         now,
		maxFailures: maxFailures,
		window:      window,
		lockFor:     lockFor,
		entries:     make(map[string]*throttleEntry),
	}
}

// Blocked reports whether key is locked and how long until it may try again.
func (l *Limiter) Blocked(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok {
		return false, 0
	}
	now := l.now()
	if now.Before(e.lockedUntil) {
		return true, e.lockedUntil.Sub(now)
	}
	return false, 0
}

// Failure records a failed attempt for key.
func (l *Limiter) Failure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.prune(now)
	e, ok := l.entries[key]
	if !ok || now.Sub(e.windowStart) > l.window {
		e = &throttleEntry{windowStart: now}
		l.entries[key] = e
	}
	e.failures++
	if e.failures >= l.maxFailures {
		e.lockedUntil = now.Add(l.lockFor)
		e.failures = 0
		e.windowStart = now
	}
}

// Success clears the counter for key.
func (l *Limiter) Success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

// prune drops stale entries periodically so memory stays bounded.
func (l *Limiter) prune(now time.Time) {
	l.ops++
	if l.ops%256 != 0 && len(l.entries) < 4096 {
		return
	}
	for k, e := range l.entries {
		if now.After(e.lockedUntil) && now.Sub(e.windowStart) > l.window {
			delete(l.entries, k)
		}
	}
}
