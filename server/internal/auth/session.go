package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"

	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/store"
)

// Session is an authenticated OIDC/basic operator who may go through RBAC.
type Session struct {
	Sub    string
	Email  string
	Name   string
	Groups []string
	Roles  []string
	Expiry time.Time
}

// SessionStore is the operator-session boundary used by the API. The database
// implementation is used by the server so sessions survive restarts and work
// across replicas; MemorySessionStore remains useful for tests and local code.
type SessionStore interface {
	TTL() time.Duration
	Create(ctx context.Context, sub, email, name string, groups, roles []string) (string, error)
	Get(ctx context.Context, token string) (Session, bool)
	Delete(ctx context.Context, token string)
}

// MemorySessionStore keeps operator login sessions in process memory. It is
// not suitable for multiple replicas.
type MemorySessionStore struct {
	mu       sync.Mutex
	sessions map[string]Session
	ttl      time.Duration
}

// NewMemorySessionStore builds an in-memory store with the given absolute
// session lifetime.
func NewMemorySessionStore(ttl time.Duration) *MemorySessionStore {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &MemorySessionStore{sessions: map[string]Session{}, ttl: ttl}
}

// TTL returns the absolute lifetime applied to new sessions.
func (s *MemorySessionStore) TTL() time.Duration {
	return s.ttl
}

// Create issues a new session token for the principal.
func (s *MemorySessionStore) Create(_ context.Context, sub, email, name string, groups, roles []string) (string, error) {
	tok, err := newSessionToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[tok] = Session{
		Sub: sub, Email: email, Name: name,
		Groups: groups, Roles: roles,
		Expiry: time.Now().Add(s.ttl),
	}
	s.prune(time.Now())
	return tok, nil
}

// Get returns the session for a token if it exists and is not expired.
func (s *MemorySessionStore) Get(_ context.Context, token string) (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	sess, ok := s.sessions[token]
	if !ok {
		return Session{}, false
	}
	if now.After(sess.Expiry) {
		delete(s.sessions, token)
		return Session{}, false
	}
	return sess, true
}

// Delete removes a token (logout).
func (s *MemorySessionStore) Delete(_ context.Context, token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

func (s *MemorySessionStore) prune(now time.Time) {
	for t, sess := range s.sessions {
		if now.After(sess.Expiry) {
			delete(s.sessions, t)
		}
	}
}

// DBSessionStore persists sessions in the shared database. It is stateless
// from the server replica's point of view and supports logout cluster-wide.
type DBSessionStore struct {
	store store.Store
	ttl   time.Duration
}

// NewDBSessionStore builds a shared session store with the given lifetime.
func NewDBSessionStore(st store.Store, ttl time.Duration) *DBSessionStore {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &DBSessionStore{store: st, ttl: ttl}
}

func (s *DBSessionStore) TTL() time.Duration { return s.ttl }

func (s *DBSessionStore) Create(ctx context.Context, sub, email, name string, groups, roles []string) (string, error) {
	tok, err := newSessionToken()
	if err != nil {
		return "", err
	}
	sess := model.OperatorSession{
		Sub: sub, Email: email, Name: name,
		Groups: groups, Roles: roles,
		Expiry: time.Now().Add(s.ttl),
	}
	if err := s.store.CreateOperatorSession(ctx, Hash(tok), sess); err != nil {
		return "", err
	}
	// Best-effort cleanup; never block a login on housekeeping.
	_ = s.store.DeleteExpiredOperatorSessions(ctx, time.Now().UTC())
	return tok, nil
}

func (s *DBSessionStore) Get(ctx context.Context, token string) (Session, bool) {
	if token == "" {
		return Session{}, false
	}
	sess, err := s.store.GetOperatorSession(ctx, Hash(token))
	if err != nil {
		return Session{}, false
	}
	if !sess.Expiry.After(time.Now()) {
		_ = s.store.DeleteOperatorSession(ctx, Hash(token))
		return Session{}, false
	}
	return Session{
		Sub: sess.Sub, Email: sess.Email, Name: sess.Name,
		Groups: sess.Groups, Roles: sess.Roles, Expiry: sess.Expiry,
	}, true
}

func (s *DBSessionStore) Delete(ctx context.Context, token string) {
	if token == "" {
		return
	}
	_ = s.store.DeleteOperatorSession(ctx, Hash(token))
}

func newSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
