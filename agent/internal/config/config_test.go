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
	t.Setenv("ANH_SERVER", "http://hub.example.test")
	t.Setenv("ANH_TOKEN", "token")
	t.Setenv("ANH_POLL_INTERVAL", "10s")
	t.Setenv("ANH_MAX_CONCURRENT_APPS", "7")

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

func TestLoadRejectsInvalidMaxConcurrentApps(t *testing.T) {
	t.Setenv("ANH_SERVER", "http://hub.example.test")
	t.Setenv("ANH_TOKEN", "token")
	t.Setenv("ANH_MAX_CONCURRENT_APPS", "0")

	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "ANH_MAX_CONCURRENT_APPS") {
		t.Fatalf("Load error = %v, want invalid ANH_MAX_CONCURRENT_APPS", err)
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
