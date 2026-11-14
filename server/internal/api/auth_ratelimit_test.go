package api

import (
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/slim/helios/server/internal/auth"
	"github.com/slim/helios/server/internal/store"
)

func basic(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

func newRateLimitedServer(t *testing.T, limit int) *Server {
	t.Helper()
	t.Setenv("HELIOS_AUTH_RATE_LIMIT", strconv.Itoa(limit))
	t.Setenv("HELIOS_AUTH_RATE_WINDOW", "1m")
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	oa := &OpAuth{Basic: func(h string) bool { return auth.CheckBasic(h, "admin", "secret") }}
	return New(st, slog.New(slog.NewTextHandler(io.Discard, nil)), oa, false)
}

func doInstances(s *Server, authorization string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestAuthRateLimitBlocksBruteForce(t *testing.T) {
	s := newRateLimitedServer(t, 2)
	bad := basic("admin", "wrong")

	if rec := doInstances(s, bad); rec.Code != http.StatusUnauthorized {
		t.Fatalf("attempt 1 = %d, want 401", rec.Code)
	}
	if rec := doInstances(s, bad); rec.Code != http.StatusUnauthorized {
		t.Fatalf("attempt 2 = %d, want 401", rec.Code)
	}
	rec := doInstances(s, bad)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("attempt 3 = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("429 response should carry Retry-After")
	}
	// Even valid credentials are rejected while the client is blocked.
	if rec := doInstances(s, basic("admin", "secret")); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("valid creds while blocked = %d, want 429", rec.Code)
	}
}

func TestRemoteIPHonoursTrustProxy(t *testing.T) {
	t.Setenv("HELIOS_TRUST_PROXY", "0")
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, false)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := s.remoteIP(req); got != "127.0.0.1" {
		t.Fatalf("untrusted proxy: remoteIP = %q, want 127.0.0.1", got)
	}

	t.Setenv("HELIOS_TRUST_PROXY", "1")
	s2 := New(st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, false)
	if got := s2.remoteIP(req); got != "203.0.113.9" {
		t.Fatalf("trusted proxy: remoteIP = %q, want 203.0.113.9", got)
	}
}

func TestAuthRateLimitKeysByForwardedIP(t *testing.T) {
	t.Setenv("HELIOS_TRUST_PROXY", "1")
	s := newRateLimitedServer(t, 1)
	bad := basic("admin", "wrong")

	attempt := func(ip string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/instances", nil)
		req.Header.Set("Authorization", bad)
		req.Header.Set("X-Forwarded-For", ip)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	if code := attempt("203.0.113.1"); code != http.StatusUnauthorized {
		t.Fatalf("first attempt = %d, want 401", code)
	}
	if code := attempt("203.0.113.1"); code != http.StatusTooManyRequests {
		t.Fatalf("blocked attempt = %d, want 429", code)
	}
	if code := attempt("203.0.113.2"); code != http.StatusUnauthorized {
		t.Fatalf("different IP = %d, want 401", code)
	}
}

func TestAuthSuccessResetsFailures(t *testing.T) {
	s := newRateLimitedServer(t, 3)
	bad := basic("admin", "wrong")

	for i := 0; i < 2; i++ {
		if rec := doInstances(s, bad); rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d = %d, want 401", i+1, rec.Code)
		}
	}
	if rec := doInstances(s, basic("admin", "secret")); rec.Code != http.StatusOK {
		t.Fatalf("valid login = %d, want 200", rec.Code)
	}
	// The counter was reset, so two more failures are still allowed.
	for i := 0; i < 2; i++ {
		if rec := doInstances(s, bad); rec.Code != http.StatusUnauthorized {
			t.Fatalf("post-reset failure %d = %d, want 401", i+1, rec.Code)
		}
	}
}
