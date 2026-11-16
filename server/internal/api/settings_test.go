package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/slim/helios/server/internal/notify"
	"github.com/slim/helios/server/internal/retention"
)

func TestSettingsReportsRuntimeAndRetention(t *testing.T) {
	s, _ := newTestServer(t)
	s.SetSettings(ServerSettings{
		Version: "test-v1", Store: "sqlite", BaseURL: "http://hub.test",
		SecretKey: true, RepoCache: true, RunnerEnabled: true, GitOps: true,
	})
	s.SetRetention(retention.New(s.store, s.log, retention.Config{
		Runs: 90 * 24 * time.Hour, Commands: 30 * 24 * time.Hour, Audit: 365 * 24 * time.Hour,
		Interval: 6 * time.Hour,
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	rec := httptest.NewRecorder()
	s.handleSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings: status %d body %s", rec.Code, rec.Body.String())
	}
	var got settingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Version != "test-v1" || got.Store != "sqlite" || got.BaseURL != "http://hub.test" {
		t.Fatalf("runtime settings = %+v", got)
	}
	if !got.Security.SecretKeySet {
		t.Fatal("secret_key_set = false, want true")
	}
	if got.Retention.RunsSeconds != int64((90*24*time.Hour)/time.Second) {
		t.Fatalf("runs_seconds = %d", got.Retention.RunsSeconds)
	}
	if !got.Features.RepoCache || !got.Features.Runner || !got.Features.GitOps || !got.Features.Events {
		t.Fatalf("features = %+v", got.Features)
	}
}

func TestWebhookTestDeliversEvent(t *testing.T) {
	s, _ := newTestServer(t)
	got := make(chan notify.Event, 1)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ev notify.Event
		if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
			t.Errorf("decode webhook: %v", err)
		}
		got <- ev
		w.WriteHeader(http.StatusNoContent)
	}))
	defer hook.Close()
	t.Setenv("HELIOS_WEBHOOK_URL", hook.URL)
	t.Setenv("HELIOS_WEBHOOK_SECRET", "s3cret")
	s.SetNotifier(notify.FromEnv(s.log))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/webhook/test", nil)
	rec := httptest.NewRecorder()
	s.handleTestWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("webhook test: status %d body %s", rec.Code, rec.Body.String())
	}
	select {
	case ev := <-got:
		if ev.Event != "test" || ev.Message == "" {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("webhook was not delivered")
	}
}
