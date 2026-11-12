package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slim/helios/agent/internal/client"
	"github.com/slim/helios/agent/internal/config"
)

func TestSSHArgs(t *testing.T) {
	cfg := config.Default()
	cfg.TunnelSSHKey = "/etc/helios/tunnel_id"
	cfg.TunnelSSHCommonArgs = "-o ConnectTimeout=7"
	cfg.TunnelLocalTarget = "127.0.0.1:2222"

	args := strings.Join(sshArgs(cfg, target{Gateway: "helios-tunnel@bastion", Port: 22001}, "/tmp/known"), " ")
	for _, want := range []string{
		"-F /dev/null",
		"-N -T",
		"-o ConnectTimeout=7",
		"-o ExitOnForwardFailure=yes",
		"-o BatchMode=yes",
		"-i /etc/helios/tunnel_id",
		"-R 127.0.0.1:22001:127.0.0.1:2222",
		"helios-tunnel@bastion",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("args %q missing %q", args, want)
		}
	}
}

func TestTargetUsesOverridesWithoutHub(t *testing.T) {
	cfg := config.Default()
	cfg.TunnelGateway = "helios-tunnel@bastion"
	cfg.TunnelPort = 22001
	m := New(cfg, client.New("http://127.0.0.1:1", "token", false), slog.New(slog.NewTextHandler(io.Discard, nil)), "test")

	got, err := m.target(context.Background())
	if err != nil {
		t.Fatalf("target: %v", err)
	}
	if got.Gateway != cfg.TunnelGateway || got.Port != cfg.TunnelPort {
		t.Fatalf("target = %+v", got)
	}
}

func TestRunReportsHealthAndStops(t *testing.T) {
	dir := t.TempDir()
	ssh := filepath.Join(dir, "fake-ssh")
	if err := os.WriteFile(ssh, []byte("#!/bin/sh\nsleep 10\n"), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}

	connected := make(chan struct{}, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agent/tunnel/health" {
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var body client.TunnelHealth
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode health: %v", err)
		}
		if body.Connected && body.Hostname == "edge-01" {
			select {
			case connected <- struct{}{}:
			default:
			}
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.Server = srv.URL
	cfg.Token = "token"
	cfg.Mode = config.ModeTunnel
	cfg.TunnelSSHKey = filepath.Join(dir, "id")
	if err := os.WriteFile(cfg.TunnelSSHKey, []byte("test key"), 0o600); err != nil {
		t.Fatalf("write test key: %v", err)
	}
	cfg.TunnelGateway = "helios-tunnel@bastion"
	cfg.TunnelPort = 22001
	cfg.StateDir = filepath.Join(dir, "state")
	cfg.Hostname = "edge-01"
	cfg.TunnelHealthInterval = 10 * time.Millisecond
	m := New(cfg, client.New(srv.URL, "token", false), slog.New(slog.NewTextHandler(io.Discard, nil)), "test")
	m.sshBin = ssh
	m.connectWait = 20 * time.Millisecond
	m.backoffMin = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()

	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("timed out waiting for connected health report")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after cancel")
	}
}
