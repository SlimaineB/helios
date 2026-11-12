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

func TestTunnelConfigAndHealth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/api/v1/agent/tunnel":
			if r.Method != http.MethodGet {
				t.Errorf("method = %s, want GET", r.Method)
			}
			_, _ = w.Write([]byte(`{"instance_id":"edge-01","name":"edge-01","gateway":"helios-tunnel@bastion","port":22001}`))
		case "/api/v1/agent/tunnel/health":
			if r.Method != http.MethodPost {
				t.Errorf("method = %s, want POST", r.Method)
			}
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "agent-token", false)
	cfg, err := c.TunnelConfig(context.Background())
	if err != nil {
		t.Fatalf("TunnelConfig: %v", err)
	}
	if cfg.Gateway != "helios-tunnel@bastion" || cfg.Port != 22001 || cfg.InstanceID != "edge-01" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if gotAuth != "Bearer agent-token" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if err := c.ReportTunnelHealth(context.Background(), TunnelHealth{
		Connected: true, Hostname: "edge-01", Platform: "linux/amd64", Version: "0.1.1",
	}); err != nil {
		t.Fatalf("ReportTunnelHealth: %v", err)
	}
}
