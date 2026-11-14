// Package ratelimit provides a small in-process failure counter used to slow
// down authentication brute-force attempts. It is intentionally dependency
// free: each server replica keeps its own counters, which is enough to bound a
// single attacker against a single process.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter blocks a key after Max failures within Window. Counters reset when
// the window elapses. A nil *Limiter is valid and never blocks.
type Limiter struct {
	mu      sync.Mutex
	max     int
	window  time.Duration
	entries map[string]entry
}

type entry struct {
	count int
	reset time.Time
}

// New builds a Limiter. A max <= 0 disables limiting (returns nil).
func New(max int, window time.Duration) *Limiter {
	if max <= 0 || window <= 0 {
		return nil
	}
	return &Limiter{
		max:     max,
		window:  window,
		entries: map[string]entry{},
	}
}

// Allow reports whether key may attempt authentication right now.
func (l *Limiter) Allow(key string) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok {
		return true
	}
	if time.Now().After(e.reset) {
		delete(l.entries, key)
		return true
	}
	return e.count < l.max
}

// Fail records a failed attempt for key and starts a new window if needed.
func (l *Limiter) Fail(key string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	e, ok := l.entries[key]
	if !ok || now.After(e.reset) {
		l.entries[key] = entry{count: 1, reset: now.Add(l.window)}
		l.prune(now)
		return
	}
	e.count++
	l.entries[key] = e
}

// Reset clears failures for key, e.g. after a successful login.
func (l *Limiter) Reset(key string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	delete(l.entries, key)
	l.mu.Unlock()
}

// RetryAfter reports how long key remains blocked (zero when not blocked).
func (l *Limiter) RetryAfter(key string) time.Duration {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok {
		return 0
	}
	d := time.Until(e.reset)
	if d < 0 {
		return 0
	}
	return d
}

// prune drops expired entries so the map cannot grow without bound. Called
// under the lock at most once per failed attempt that starts a new window.
func (l *Limiter) prune(now time.Time) {
	if len(l.entries) < 4096 {
		return
	}
	for k, e := range l.entries {
		if now.After(e.reset) {
			delete(l.entries, k)
		}
	}
}
