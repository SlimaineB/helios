package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultMaxConcurrentApps(t *testing.T) {
	if got := Default().MaxConcurrentApps; got != 4 {
		t.Fatalf("Default().MaxConcurrentApps = %d, want 4", got)
	}
}

func TestLoadMaxConcurrentAppsFromEnv(t *testing.T) {
	t.Setenv("HELIOS_SERVER", "http://hub.example.test")
	t.Setenv("HELIOS_TOKEN", "token")
	t.Setenv("HELIOS_POLL_INTERVAL", "10s")
	t.Setenv("HELIOS_MAX_CONCURRENT_APPS", "7")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MaxConcurrentApps != 7 {
		t.Fatalf("MaxConcurrentApps = %d, want 7", cfg.MaxConcurrentApps)
	}
	if cfg.PollInterval != 10*time.Second {
		t.Fatalf("PollInterval = %v, want 10s", cfg.PollInterval)
	}
}

func TestLoadLegacyEnvAliases(t *testing.T) {
	t.Setenv("HELIOS_SERVER", "")
	t.Setenv("HELIOS_TOKEN", "")
	t.Setenv("ANSIBLEHUB_SERVER", "http://legacy.example.test")
	t.Setenv("ANH_TOKEN", "legacy-token")
	t.Setenv("AHUB_POLL_INTERVAL", "45s")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server != "http://legacy.example.test" {
		t.Fatalf("Server = %q, want legacy alias value", cfg.Server)
	}
	if cfg.Token != "legacy-token" {
		t.Fatalf("Token = %q, want legacy alias value", cfg.Token)
	}
	if cfg.PollInterval != 45*time.Second {
		t.Fatalf("PollInterval = %v, want 45s", cfg.PollInterval)
	}
}

func TestLoadRejectsInvalidMaxConcurrentApps(t *testing.T) {
	t.Setenv("HELIOS_SERVER", "http://hub.example.test")
	t.Setenv("HELIOS_TOKEN", "token")
	t.Setenv("HELIOS_MAX_CONCURRENT_APPS", "0")

	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "HELIOS_MAX_CONCURRENT_APPS") {
		t.Fatalf("Load error = %v, want invalid HELIOS_MAX_CONCURRENT_APPS", err)
	}
}

func TestValidateRejectsNegativeMaxConcurrentApps(t *testing.T) {
	cfg := Default()
	cfg.Server = "http://hub.example.test"
	cfg.Token = "token"
	cfg.MaxConcurrentApps = -1

	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "max_concurrent_apps") {
		t.Fatalf("Validate error = %v, want max_concurrent_apps error", err)
	}
}

func TestValidateAllowsZeroMaxConcurrentAppsAsDefault(t *testing.T) {
	cfg := Default()
	cfg.Server = "http://hub.example.test"
	cfg.Token = "token"
	cfg.MaxConcurrentApps = 0

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate with 0 (use default): %v", err)
	}
}

func TestLoadTunnelModeFromEnv(t *testing.T) {
	t.Setenv("HELIOS_SERVER", "http://hub.example.test")
	t.Setenv("HELIOS_TOKEN", "token")
	t.Setenv("HELIOS_MODE", "tunnel")
	t.Setenv("HELIOS_TUNNEL_SSH_KEY", "/etc/helios/tunnel_id")
	t.Setenv("HELIOS_TUNNEL_HEALTH_INTERVAL", "15s")
	t.Setenv("HELIOS_TUNNEL_PORT", "22001")
	t.Setenv("HELIOS_TUNNEL_LOCAL_TARGET", "127.0.0.1:2222")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.IsTunnel() {
		t.Fatalf("mode = %q, want tunnel", cfg.Mode)
	}
	if cfg.TunnelSSHKey != "/etc/helios/tunnel_id" {
		t.Fatalf("TunnelSSHKey = %q", cfg.TunnelSSHKey)
	}
	if cfg.TunnelHealthInterval != 15*time.Second {
		t.Fatalf("TunnelHealthInterval = %v", cfg.TunnelHealthInterval)
	}
	if cfg.TunnelPort != 22001 || cfg.TunnelLocalTarget != "127.0.0.1:2222" {
		t.Fatalf("tunnel target = %d/%q", cfg.TunnelPort, cfg.TunnelLocalTarget)
	}
}

func TestValidateTunnelRequiresKey(t *testing.T) {
	cfg := Default()
	cfg.Server = "http://hub.example.test"
	cfg.Token = "token"
	cfg.Mode = ModeTunnel

	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "tunnel_ssh_key") {
		t.Fatalf("Validate error = %v, want tunnel_ssh_key", err)
	}
}

func TestValidateRejectsUnknownMode(t *testing.T) {
	cfg := Default()
	cfg.Server = "http://hub.example.test"
	cfg.Token = "token"
	cfg.Mode = "sidecar"

	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "mode must be") {
		t.Fatalf("Validate error = %v, want mode error", err)
	}
}

func TestLoadTunnelModeFromYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.yaml")
	body := `server: http://hub.example.test
token: token
poll_interval: 5m
mode: tunnel
tunnel_ssh_key: /tmp/tunnel_id
tunnel_local_target: 127.0.0.1:2222
tunnel_health_interval: 15s
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.IsTunnel() {
		t.Fatalf("mode = %q, want tunnel", cfg.Mode)
	}
	if cfg.TunnelHealthInterval != 15*time.Second {
		t.Fatalf("TunnelHealthInterval = %v, want 15s", cfg.TunnelHealthInterval)
	}
	if cfg.TunnelLocalTarget != "127.0.0.1:2222" {
		t.Fatalf("TunnelLocalTarget = %q", cfg.TunnelLocalTarget)
	}
}
