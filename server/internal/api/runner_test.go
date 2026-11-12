package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/store"
)

func createInstance(t *testing.T, st store.Store, id, mode string, port int) model.Instance {
	t.Helper()
	inst, err := st.CreateInstance(context.Background(), model.Instance{
		ID:            id,
		Name:          id,
		TokenHash:     "hash",
		ExecutionMode: mode,
		TunnelPort:    port,
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	return inst
}

func TestCreateTunnelInstanceAllocatesPort(t *testing.T) {
	s, st := newTestServer(t)
	s.SetTunnelConfig("helios-tunnel@bastion.example.com", 22000, 22010)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances",
		strings.NewReader(`{"name":"edge-01","project":"","execution_mode":"tunnel"}`))
	rec := httptest.NewRecorder()
	s.handleCreateInstance(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status %d body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Instance model.Instance `json:"instance"`
		Token    string         `json:"token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("expected a bootstrap token")
	}
	if resp.Instance.ExecutionMode != model.ModeTunnel {
		t.Fatalf("mode = %q, want tunnel", resp.Instance.ExecutionMode)
	}
	if resp.Instance.TunnelPort != 22000 {
		t.Fatalf("port = %d, want 22000", resp.Instance.TunnelPort)
	}
	if resp.Instance.TunnelGateway != "helios-tunnel@bastion.example.com" {
		t.Fatalf("gateway = %q", resp.Instance.TunnelGateway)
	}

	// A second tunnel instance gets the next free port.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/instances",
		strings.NewReader(`{"name":"edge-02","project":"","execution_mode":"tunnel"}`))
	rec = httptest.NewRecorder()
	s.handleCreateInstance(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("second create: status %d body %s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Instance.TunnelPort != 22001 {
		t.Fatalf("second port = %d, want 22001", resp.Instance.TunnelPort)
	}

	tunnels, err := st.ListTunnelInstances(context.Background())
	if err != nil {
		t.Fatalf("list tunnels: %v", err)
	}
	if len(tunnels) != 2 {
		t.Fatalf("tunnel instances = %d, want 2", len(tunnels))
	}
}

func TestRunnerAPIRequiresToken(t *testing.T) {
	s, st := newTestServer(t)
	createInstance(t, st, "t1", model.ModeTunnel, 22000)

	// Disabled until a token is configured.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runner/instances", nil)
	rec := httptest.NewRecorder()
	s.runnerAuth(s.handleRunnerInstances)(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled: status %d, want 503", rec.Code)
	}

	s.SetRunnerToken("runner-secret")
	rec = httptest.NewRecorder()
	s.runnerAuth(s.handleRunnerInstances)(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing bearer: status %d, want 401", rec.Code)
	}

	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	s.runnerAuth(s.handleRunnerInstances)(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad bearer: status %d, want 401", rec.Code)
	}

	req.Header.Set("Authorization", "Bearer runner-secret")
	rec = httptest.NewRecorder()
	s.runnerAuth(s.handleRunnerInstances)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authorized: status %d body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"tunnel_port":22000`) {
		t.Fatalf("response misses tunnel port: %s", rec.Body.String())
	}
}

func TestRunnerInstanceAuthRejectsAgentMode(t *testing.T) {
	s, st := newTestServer(t)
	s.SetRunnerToken("runner-secret")
	createInstance(t, st, "agent-1", model.ModeAgent, 0)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/runner/instances/agent-1/checkin",
		strings.NewReader(`{}`))
	req.SetPathValue("id", "agent-1")
	req.Header.Set("Authorization", "Bearer runner-secret")
	rec := httptest.NewRecorder()
	s.runnerInstanceAuth(s.handleCheckin)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", rec.Code)
	}
}

func TestTunnelCheckinSetsLastTunnelAt(t *testing.T) {
	s, st := newTestServer(t)
	s.SetRunnerToken("runner-secret")
	createInstance(t, st, "tunnel-1", model.ModeTunnel, 22000)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/runner/instances/tunnel-1/checkin",
		strings.NewReader(`{"version":"0.1.1","hostname":"edge-01","platform":"reverse-ssh-tunnel"}`))
	req.SetPathValue("id", "tunnel-1")
	req.Header.Set("Authorization", "Bearer runner-secret")
	rec := httptest.NewRecorder()
	s.runnerInstanceAuth(s.handleCheckin)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("checkin: status %d body %s", rec.Code, rec.Body.String())
	}

	got, err := st.GetInstance(context.Background(), "tunnel-1")
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	if got.LastTunnelAt.IsZero() {
		t.Fatal("last_tunnel_at was not set")
	}
	if got.LastSeen.IsZero() {
		t.Fatal("last_seen was not set")
	}
	if time.Since(got.LastTunnelAt) > time.Minute {
		t.Fatalf("last_tunnel_at too old: %v", got.LastTunnelAt)
	}
	if got.Hostname != "edge-01" || got.Platform != "reverse-ssh-tunnel" {
		t.Fatalf("hostname/platform = %q/%q", got.Hostname, got.Platform)
	}
}
