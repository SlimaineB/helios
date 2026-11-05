package config

import (
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
