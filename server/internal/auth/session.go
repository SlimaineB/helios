package auth

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
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

// SessionStore keeps operator login sessions in memory (browser JWT-session
// holder). Sufficient for single-instance deployments; multi-instance HA needs
// a shared store such as Redis — sessions here die on restart.
type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]Session
	ttl      time.Duration
}

// NewSessionStore builds a store with the given absolute session lifetime.
func NewSessionStore(ttl time.Duration) *SessionStore {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &SessionStore{sessions: map[string]Session{}, ttl: ttl}
}

// TTL returns the absolute lifetime applied to new sessions.
func (s *SessionStore) TTL() time.Duration {
	return s.ttl
}

// Create issues a new session token for the principal.
func (s *SessionStore) Create(sub, email, name string, groups, roles []string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := base64.RawURLEncoding.EncodeToString(b)
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
func (s *SessionStore) Get(token string) (Session, bool) {
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
func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

func (s *SessionStore) prune(now time.Time) {
	for t, sess := range s.sessions {
		if now.After(sess.Expiry) {
			delete(s.sessions, t)
		}
	}
}
