package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

func TestListRunnerInstances(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/api/v1/runner/instances" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		_, _ = w.Write([]byte(`{"instances":[{"id":"web-1","name":"web-1","project":"prod","execution_mode":"ssh","inventory_host":"web-1"}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "runner-token", false)
	insts, err := c.ListRunnerInstances(context.Background())
	if err != nil {
		t.Fatalf("ListRunnerInstances: %v", err)
	}
	if len(insts) != 1 || insts[0].InventoryHost != "web-1" || insts[0].ExecutionMode != "ssh" {
		t.Fatalf("unexpected instances: %+v", insts)
	}
	if gotAuth != "Bearer runner-token" {
		t.Fatalf("authorization = %q", gotAuth)
	}
}

func TestControlPlaneCallsAreBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "t", false)
	c.requestTimeout = 20 * time.Millisecond

	start := time.Now()
	if err := c.ReportStatus(context.Background(), StatusReport{}); err == nil {
		t.Fatal("expected timeout error from a hanging control-plane call")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("control-plane call took %v, want it bounded by requestTimeout", elapsed)
	}
}
