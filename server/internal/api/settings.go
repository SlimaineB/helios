package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/slim/helios/server/internal/notify"
	"github.com/slim/helios/server/internal/store"
)

// ServerSettings is the non-secret runtime configuration rendered on the
// Settings page. Auth and webhook state are read from the live opAuth/notifier.
type ServerSettings struct {
	Version       string
	Store         string
	BaseURL       string
	TLS           bool
	SecretKey     bool
	RepoCache     bool
	TunnelGateway string
	RunnerEnabled bool
	GitOps        bool
}

type authSettings struct {
	BasicAdmin bool `json:"basic_admin"`
	OIDC       bool `json:"oidc"`
}

type securitySettings struct {
	TLS              bool `json:"tls"`
	SecretKeySet     bool `json:"secret_key_set"`
	EncryptedSecrets bool `json:"encrypted_secrets"`
}

type webhookSettings struct {
	Configured bool   `json:"configured"`
	Endpoint   string `json:"endpoint,omitempty"`
	Signed     bool   `json:"signed"`
	TimeoutSec int64  `json:"timeout_seconds"`
}

type retentionSettings struct {
	RunsSeconds     int64      `json:"runs_seconds"`
	CommandsSeconds int64      `json:"commands_seconds"`
	AuditSeconds    int64      `json:"audit_seconds"`
	IntervalSeconds int64      `json:"interval_seconds"`
	LastRun         *time.Time `json:"last_run,omitempty"`
	DeletedRuns     int64      `json:"deleted_runs"`
	DeletedCommands int64      `json:"deleted_commands"`
	DeletedAudit    int64      `json:"deleted_audit"`
	Error           string     `json:"error,omitempty"`
}

type featureSettings struct {
	RepoCache     bool   `json:"repo_cache"`
	Runner        bool   `json:"runner"`
	GitOps        bool   `json:"gitops"`
	TunnelGateway string `json:"tunnel_gateway,omitempty"`
	Events        bool   `json:"events"`
}

const (
	scanIntervalSetting = "scan_interval_seconds"
	scanJitterSetting   = "scan_jitter_percent"
	defaultScanSeconds  = 30
	defaultScanJitter   = 20
	minScanSeconds      = 10
	maxScanSeconds      = 86400
)

type scanningSettings struct {
	IntervalSeconds int64 `json:"interval_seconds"`
	JitterPercent   int   `json:"jitter_percent"`
	MinSeconds      int   `json:"min_seconds"`
	MaxSeconds      int   `json:"max_seconds"`
}

// scanningSettings returns the fleet-wide scan policy, falling back to defaults
// when the operator has never saved it.
func (s *Server) scanningSettings(ctx context.Context) (scanningSettings, error) {
	out := scanningSettings{
		IntervalSeconds: defaultScanSeconds,
		JitterPercent:   defaultScanJitter,
		MinSeconds:      minScanSeconds,
		MaxSeconds:      maxScanSeconds,
	}
	if v, err := s.store.GetRuntimeSetting(ctx, scanIntervalSetting); err == nil {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= minScanSeconds && n <= maxScanSeconds {
			out.IntervalSeconds = n
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return out, err
	}
	if v, err := s.store.GetRuntimeSetting(ctx, scanJitterSetting); err == nil {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 50 {
			out.JitterPercent = n
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return out, err
	}
	return out, nil
}

type settingsResponse struct {
	Version   string            `json:"version"`
	Store     string            `json:"store"`
	BaseURL   string            `json:"base_url"`
	Auth      authSettings      `json:"auth"`
	Security  securitySettings  `json:"security"`
	Webhook   webhookSettings   `json:"webhook"`
	Retention retentionSettings `json:"retention"`
	Features  featureSettings   `json:"features"`
	Scanning  scanningSettings  `json:"scanning"`
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "settings.get") {
		return
	}
	scanning, err := s.scanningSettings(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	resp := settingsResponse{
		Version: s.settings.Version,
		Store:   s.settings.Store,
		BaseURL: s.settings.BaseURL,
		Security: securitySettings{
			TLS:              s.settings.TLS,
			SecretKeySet:     s.settings.SecretKey,
			EncryptedSecrets: s.settings.SecretKey,
		},
		Features: featureSettings{
			RepoCache:     s.settings.RepoCache,
			Runner:        s.settings.RunnerEnabled,
			GitOps:        s.settings.GitOps,
			TunnelGateway: s.settings.TunnelGateway,
			Events:        true,
		},
		Scanning: scanning,
	}
	if s.opAuth != nil {
		resp.Auth.BasicAdmin = s.opAuth.Basic != nil
		resp.Auth.OIDC = s.opAuth.OIDC != nil
	}
	if s.notifier != nil {
		resp.Webhook.Configured = s.notifier.Configured()
		resp.Webhook.Endpoint = s.notifier.Endpoint()
		resp.Webhook.Signed = s.notifier.Signed()
		resp.Webhook.TimeoutSec = int64(s.notifier.Timeout() / time.Second)
	}
	if s.retention != nil {
		cfg := s.retention.Config()
		stats := s.retention.Stats()
		resp.Retention = retentionSettings{
			RunsSeconds:     int64(cfg.Runs / time.Second),
			CommandsSeconds: int64(cfg.Commands / time.Second),
			AuditSeconds:    int64(cfg.Audit / time.Second),
			IntervalSeconds: int64(cfg.Interval / time.Second),
			DeletedRuns:     stats.DeletedRuns,
			DeletedCommands: stats.DeletedCommands,
			DeletedAudit:    stats.DeletedAudit,
			Error:           stats.Error,
		}
		if !stats.LastRun.IsZero() {
			last := stats.LastRun
			resp.Retention.LastRun = &last
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type scanningUpdate struct {
	IntervalSeconds *int64 `json:"interval_seconds"`
	JitterPercent   *int   `json:"jitter_percent"`
}

func (s *Server) handleUpdateScanning(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "settings.update") {
		return
	}
	var body scanningUpdate
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	cur, err := s.scanningSettings(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if body.IntervalSeconds != nil {
		cur.IntervalSeconds = *body.IntervalSeconds
	}
	if body.JitterPercent != nil {
		cur.JitterPercent = *body.JitterPercent
	}
	if cur.IntervalSeconds < minScanSeconds || cur.IntervalSeconds > maxScanSeconds {
		writeErr(w, http.StatusBadRequest, "interval_seconds must be between 10 and 86400")
		return
	}
	if cur.JitterPercent < 0 || cur.JitterPercent > 50 {
		writeErr(w, http.StatusBadRequest, "jitter_percent must be between 0 and 50")
		return
	}
	if err := s.store.SetRuntimeSetting(r.Context(), scanIntervalSetting, strconv.FormatInt(cur.IntervalSeconds, 10)); err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.store.SetRuntimeSetting(r.Context(), scanJitterSetting, strconv.Itoa(cur.JitterPercent)); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.publishEvent(hubEvent{Type: "settings", Status: "scanning"})
	writeJSON(w, http.StatusOK, cur)
}

func (s *Server) handleTestWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "settings.update") {
		return
	}
	if !s.notifier.Configured() {
		writeErr(w, http.StatusConflict, "no webhook is configured; set HELIOS_WEBHOOK_URL and restart the server")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	err := s.notifier.SendSync(ctx, notify.Event{
		Event:   "test",
		Status:  "ok",
		Message: "Helios webhook test from the Settings page",
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, "webhook test failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "Test event delivered to " + s.notifier.Endpoint(),
	})
}
