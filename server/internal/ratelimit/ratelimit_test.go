package ratelimit

import (
	"testing"
	"time"
)

func TestNilLimiterAllows(t *testing.T) {
	var l *Limiter
	if !l.Allow("ip") {
		t.Fatal("nil limiter must allow")
	}
	l.Fail("ip") // must not panic
	l.Reset("ip")
}

func TestBlocksAfterMaxFailures(t *testing.T) {
	l := New(2, time.Minute)
	if !l.Allow("ip") {
		t.Fatal("first attempt should be allowed")
	}
	l.Fail("ip")
	if !l.Allow("ip") {
		t.Fatal("second attempt should be allowed")
	}
	l.Fail("ip")
	if l.Allow("ip") {
		t.Fatal("third attempt should be blocked")
	}
	if l.RetryAfter("ip") <= 0 {
		t.Fatal("blocked key should report a retry delay")
	}
}

func TestResetClearsFailures(t *testing.T) {
	l := New(1, time.Minute)
	l.Fail("ip")
	if l.Allow("ip") {
		t.Fatal("should be blocked")
	}
	l.Reset("ip")
	if !l.Allow("ip") {
		t.Fatal("reset should clear the block")
	}
}

func TestWindowExpiry(t *testing.T) {
	l := New(1, 10*time.Millisecond)
	l.Fail("ip")
	if l.Allow("ip") {
		t.Fatal("should be blocked within the window")
	}
	time.Sleep(20 * time.Millisecond)
	if !l.Allow("ip") {
		t.Fatal("block should expire after the window")
	}
}

func TestKeysAreIndependent(t *testing.T) {
	l := New(1, time.Minute)
	l.Fail("a")
	if l.Allow("a") {
		t.Fatal("a should be blocked")
	}
	if !l.Allow("b") {
		t.Fatal("b should not be affected by a")
	}
}
