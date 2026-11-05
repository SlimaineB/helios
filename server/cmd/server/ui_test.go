package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func testUI(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("index"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("app"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestUIMiddleware(t *testing.T) {
	ui := testUI(t)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	h := uiMiddleware(next, ui, false)

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantBody   string
		wantCache  string
	}{
		{"asset", "/assets/app.js", http.StatusOK, "app", "public, max-age=31536000, immutable"},
		{"spa route", "/instances/one", http.StatusOK, "index", "no-cache"},
		{"spa route trailing slash", "/instances/", http.StatusOK, "index", "no-cache"},
		{"missing asset", "/assets/missing.js", http.StatusNotFound, "", "no-cache"},
		{"api passthrough", "/api/v1/health", http.StatusTeapot, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if tc.wantBody != "" && rec.Body.String() != tc.wantBody {
				t.Fatalf("body = %q, want %q", rec.Body.String(), tc.wantBody)
			}
			if got := rec.Header().Get("Cache-Control"); got != tc.wantCache {
				t.Fatalf("Cache-Control = %q, want %q", got, tc.wantCache)
			}
			if tc.name != "api passthrough" && rec.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("static response is missing security headers")
			}
		})
	}
}

func TestUIMiddlewareRejectsTraversal(t *testing.T) {
	ui := testUI(t)
	h := uiMiddleware(http.NotFoundHandler(), ui, false)
	rec := httptest.NewRecorder()
	req := &http.Request{Method: http.MethodGet, URL: &url.URL{Path: "/../secret"}}
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
