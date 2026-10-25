package loop

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/slim/ansiblehub/agent/internal/client"
	"github.com/slim/ansiblehub/agent/internal/config"
)

func testAgent(base string) *Agent {
	return New(
		config.Config{Server: base, Token: "t", PollInterval: 5 * time.Minute, StateDir: "/tmp"},
		client.New(base, "t", false),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
}

func TestIntervalFor(t *testing.T) {
	a := testAgent("http://example.invalid")
	if got := intervalFor(client.Application{SyncInterval: 60}, a.pollInterval()); got != time.Minute {
		t.Fatalf("intervalFor(60) = %v, want 1m", got)
	}
	if got := intervalFor(client.Application{}, a.pollInterval()); got != 5*time.Minute {
		t.Fatalf("intervalFor(inherit) = %v, want 5m", got)
	}
}

func TestNextWakeUsesShortestAppInterval(t *testing.T) {
	a := testAgent("http://example.invalid")
	if got := a.nextWake(); got != 5*time.Minute {
		t.Fatalf("nextWake with no apps = %v, want 5m", got)
	}

	a.states[1] = &appState{app: client.Application{SyncInterval: 60}, lastCheck: time.Now()}
	if got := a.nextWake(); got > time.Minute || got < 50*time.Second {
		t.Fatalf("nextWake with 60s app = %v, want ~60s", got)
	}

	delete(a.states, 1)
	a.states[2] = &appState{app: client.Application{}, lastCheck: time.Now()}
	if got := a.nextWake(); got > 5*time.Minute || got < 4*time.Minute {
		t.Fatalf("nextWake with inherited app = %v, want ~5m", got)
	}

	a.states[3] = &appState{app: client.Application{SyncInterval: 60}}
	if got := a.nextWake(); got != time.Second {
		t.Fatalf("nextWake with never-checked app = %v, want 1s", got)
	}
}

func TestPassDoesNothingWhenServerIsDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	a := testAgent(srv.URL)
	st := &appState{
		app:  client.Application{ID: 1, Name: "demo", RepoURL: "http://repo/demo.git", AutoHeal: true},
		root: t.TempDir(),
	}
	a.states[1] = st

	a.pass(context.Background())

	if !st.lastCheck.IsZero() {
		t.Fatalf("app was processed while the server was down")
	}
}
