package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/slim/helios/server/internal/auth"
	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/store"
)

func createTunnelAgent(t *testing.T, st store.Store, id, token string, port int) {
	t.Helper()
	if _, err := st.CreateInstance(context.Background(), model.Instance{
		ID: id, Name: id, TokenHash: auth.Hash(token), ExecutionMode: model.ModeTunnel, TunnelPort: port,
	}); err != nil {
		t.Fatalf("create instance: %v", err)
	}
}

func TestTunnelAgentConfig(t *testing.T) {
	s, st := newTestServer(t)
	s.SetTunnelConfig("helios-tunnel@bastion.example.com", 22000, 22010)
	createTunnelAgent(t, st, "edge-01", "agent-token", 22001)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/tunnel", nil)
	req.Header.Set("Authorization", "Bearer agent-token")
	rec := httptest.NewRecorder()
	s.agentAuth(s.handleAgentTunnelConfig)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`"instance_id":"edge-01"`, `"gateway":"helios-tunnel@bastion.example.com"`, `"port":22001`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("response missing %s: %s", want, rec.Body.String())
		}
	}
}

func TestTunnelAgentConfigRejectsAgentModeAndMissingGateway(t *testing.T) {
	s, st := newTestServer(t)
	s.SetTunnelConfig("", 22000, 22010)
	if _, err := st.CreateInstance(context.Background(), model.Instance{
		ID: "agent-1", Name: "agent-1", TokenHash: auth.Hash("agent-token"), ExecutionMode: model.ModeAgent,
	}); err != nil {
		t.Fatalf("create agent instance: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/tunnel", nil)
	req.Header.Set("Authorization", "Bearer agent-token")
	rec := httptest.NewRecorder()
	s.agentAuth(s.handleAgentTunnelConfig)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("agent mode: status %d, want 403", rec.Code)
	}

	createTunnelAgent(t, st, "edge-01", "tunnel-token", 22001)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/agent/tunnel", nil)
	req.Header.Set("Authorization", "Bearer tunnel-token")
	rec = httptest.NewRecorder()
	s.agentAuth(s.handleAgentTunnelConfig)(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing gateway: status %d, want 503", rec.Code)
	}
}

func TestTunnelAgentHealthUpdatesLiveness(t *testing.T) {
	s, st := newTestServer(t)
	createTunnelAgent(t, st, "edge-01", "agent-token", 22001)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/tunnel/health",
		strings.NewReader(`{"connected":true,"hostname":"edge-01","platform":"linux/amd64","version":"0.1.1"}`))
	req.Header.Set("Authorization", "Bearer agent-token")
	rec := httptest.NewRecorder()
	s.agentAuth(s.handleAgentTunnelHealth)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}

	got, err := st.GetInstance(context.Background(), "edge-01")
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	if got.LastSeen.IsZero() || got.LastTunnelAt.IsZero() {
		t.Fatalf("liveness not updated: last_seen=%v last_tunnel_at=%v", got.LastSeen, got.LastTunnelAt)
	}
	if got.Hostname != "edge-01" || got.Platform != "linux/amd64" || got.Version != "0.1.1" {
		t.Fatalf("metadata = %q/%q/%q", got.Hostname, got.Platform, got.Version)
	}
}
