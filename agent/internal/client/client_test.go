package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/health" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ok.Close()
	if err := New(ok.URL, "t", false).Health(context.Background()); err != nil {
		t.Fatalf("health (ok): %v", err)
	}

	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
	}))
	defer down.Close()
	if err := New(down.URL, "t", false).Health(context.Background()); err == nil {
		t.Fatal("health (503): want error")
	}

	dead := New("http://127.0.0.1:1", "t", false)
	if err := dead.Health(context.Background()); err == nil {
		t.Fatal("health (unreachable): want error")
	}
}
