package repocache

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLocalLockerSerializes(t *testing.T) {
	l := NewLocalLocker()
	release, err := l.AcquireLock(context.Background(), "repo")
	if err != nil {
		t.Fatal(err)
	}
	acquired := make(chan struct{})
	go func() {
		r, err := l.AcquireLock(context.Background(), "repo")
		if err != nil {
			t.Errorf("second acquire: %v", err)
			return
		}
		close(acquired)
		r()
	}()

	select {
	case <-acquired:
		t.Fatal("lock was acquired concurrently")
	case <-time.After(20 * time.Millisecond):
	}
	release()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("lock was not released")
	}
}

func TestLocalLockerHonorsContext(t *testing.T) {
	l := NewLocalLocker()
	release, err := l.AcquireLock(context.Background(), "repo")
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.AcquireLock(ctx, "repo"); !errors.Is(err, context.Canceled) {
		t.Fatalf("acquire error = %v, want context.Canceled", err)
	}
}
