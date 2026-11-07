package auth

import (
	"context"
	"testing"
	"time"
)

func TestSessionStoreKeepsAbsoluteExpiry(t *testing.T) {
	s := NewMemorySessionStore(time.Hour)
	tok, err := s.Create(context.Background(), "sub", "a@example.com", "A", []string{"g"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	expiry := s.sessions[tok].Expiry
	s.mu.Unlock()

	sess, ok := s.Get(context.Background(), tok)
	if !ok {
		t.Fatal("session not found")
	}
	if !sess.Expiry.Equal(expiry) {
		t.Fatalf("expiry changed from %s to %s", expiry, sess.Expiry)
	}
}

func TestSessionStoreExpiresAndDeletes(t *testing.T) {
	s := NewMemorySessionStore(time.Hour)
	tok, err := s.Create(context.Background(), "sub", "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	sess := s.sessions[tok]
	sess.Expiry = time.Now().Add(-time.Second)
	s.sessions[tok] = sess
	s.mu.Unlock()

	if _, ok := s.Get(context.Background(), tok); ok {
		t.Fatal("expired session accepted")
	}
	s.mu.Lock()
	_, exists := s.sessions[tok]
	s.mu.Unlock()
	if exists {
		t.Fatal("expired session was not deleted")
	}
}
