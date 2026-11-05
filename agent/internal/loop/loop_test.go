package loop

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/slim/helios/agent/internal/client"
	"github.com/slim/helios/agent/internal/config"
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

func TestMaxConcurrent(t *testing.T) {
	a := testAgent("http://example.invalid")
	if got := a.maxConcurrent(); got != 4 {
		t.Fatalf("zero config maxConcurrent = %d, want default 4", got)
	}
	a.cfg.MaxConcurrentApps = 2
	if got := a.maxConcurrent(); got != 2 {
		t.Fatalf("configured maxConcurrent = %d, want 2", got)
	}
}

func TestRunPoolBoundsConcurrency(t *testing.T) {
	var current, peak int32
	items := make([]int, 12)
	runPool(3, items, func(int) {
		n := atomic.AddInt32(&current, 1)
		for {
			old := atomic.LoadInt32(&peak)
			if n <= old || atomic.CompareAndSwapInt32(&peak, old, n) {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
		atomic.AddInt32(&current, -1)
	})

	if got := atomic.LoadInt32(&peak); got != 3 {
		t.Fatalf("peak concurrency = %d, want 3", got)
	}
	if got := atomic.LoadInt32(&current); got != 0 {
		t.Fatalf("current concurrency after pool = %d, want 0", got)
	}
}

func TestRunPoolProcessesEveryItem(t *testing.T) {
	seen := make([]int32, 20)
	items := make([]int, len(seen))
	for i := range items {
		items[i] = i
	}
	runPool(4, items, func(i int) {
		atomic.AddInt32(&seen[i], 1)
	})
	for i, n := range seen {
		if n != 1 {
			t.Fatalf("item %d processed %d times, want 1", i, n)
		}
	}
}

func TestGroupCommandsPreservesPerAppOrder(t *testing.T) {
	cmds := []client.Command{
		{ID: 1, AppID: 2, Action: "apply"},
		{ID: 2, AppID: 1, Action: "check"},
		{ID: 3, AppID: 2, Action: "check"},
		{ID: 4, AppID: 3, Action: "apply"},
	}
	order, groups := groupCommands(cmds)
	if len(order) != 3 || order[0] != 2 || order[1] != 1 || order[2] != 3 {
		t.Fatalf("order = %v, want [2 1 3]", order)
	}
	if got := len(groups[2]); got != 2 || groups[2][0].ID != 1 || groups[2][1].ID != 3 {
		t.Fatalf("app 2 group = %+v, want IDs [1 3]", groups[2])
	}
	if got := len(groups[1]); got != 1 || groups[1][0].ID != 2 {
		t.Fatalf("app 1 group = %+v, want ID [2]", groups[1])
	}
}
