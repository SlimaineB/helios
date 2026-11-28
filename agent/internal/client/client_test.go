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

func TestRunnerJobCalls(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if got := r.Header.Get("Authorization"); got != "Bearer runner-token" {
			t.Errorf("authorization = %q", got)
		}
		switch r.URL.Path {
		case "/api/v1/runner/heartbeat":
			w.WriteHeader(http.StatusNoContent)
		case "/api/v1/runner/jobs/claim":
			_, _ = w.Write([]byte(`{"job":{"id":7,"app_id":3,"mode":"check","status":"running"},"application":{"id":3,"name":"web","inventory_source":"hub"}}`))
		case "/api/v1/runner/jobs/7/result":
			_, _ = w.Write([]byte(`{"id":11,"app_id":3,"status":"ok"}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewRunner(srv.URL, "runner-token", false)
	ctx := context.Background()
	if err := c.RunnerHeartbeat(ctx, "runner-eu", []string{"eu"}, "test", nil); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	job, ok, err := c.ClaimRunnerJob(ctx, "runner-eu", []string{"eu"})
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if job.ID != 7 || job.Application.ID != 3 || job.Application.Name != "web" {
		t.Fatalf("unexpected job: %+v", job)
	}
	if err := c.ReportRunnerJobResult(ctx, job.ID, "runner-eu", RunnerJobResult{Status: "ok"}); err != nil {
		t.Fatalf("result: %v", err)
	}
	if len(paths) != 3 {
		t.Fatalf("paths = %v", paths)
	}
}

func TestClaimRunnerJobEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	job, ok, err := NewRunner(srv.URL, "t", false).ClaimRunnerJob(context.Background(), "runner-eu", nil)
	if err != nil || ok || job.ID != 0 {
		t.Fatalf("job=%+v ok=%v err=%v", job, ok, err)
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
