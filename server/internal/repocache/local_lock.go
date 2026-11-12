package repocache

import (
	"context"
	"sync"
)

// LocalLocker serializes cache work inside one replica. It is the right locker
// for per-replica ephemeral caches: there is no shared directory to protect and
// no database connection needs to be held for the duration of a git fetch.
type LocalLocker struct {
	mu    sync.Mutex
	locks map[string]*localLock
}

type localLock struct {
	ch   chan struct{}
	refs int
}

func NewLocalLocker() *LocalLocker {
	return &LocalLocker{locks: map[string]*localLock{}}
}

func (l *LocalLocker) AcquireLock(ctx context.Context, key string) (func(), error) {
	l.mu.Lock()
	entry := l.locks[key]
	if entry == nil {
		entry = &localLock{ch: make(chan struct{}, 1)}
		l.locks[key] = entry
	}
	entry.refs++
	l.mu.Unlock()

	dropRef := func() {
		l.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(l.locks, key)
		}
		l.mu.Unlock()
	}

	select {
	case entry.ch <- struct{}{}:
		var once sync.Once
		return func() {
			once.Do(func() {
				<-entry.ch
				dropRef()
			})
		}, nil
	case <-ctx.Done():
		dropRef()
		return nil, ctx.Err()
	}
}
