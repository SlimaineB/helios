package retention

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/store"
)

func TestRunOncePrunesOldRuns(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "retention.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	if _, err := st.CreateInstance(ctx, model.Instance{ID: "i1", Name: "host1", TokenHash: "h1"}); err != nil {
		t.Fatalf("create instance: %v", err)
	}
	now := time.Now().UTC()
	old := model.Run{InstanceID: "i1", AppID: 1, Mode: "check", Status: "ok",
		StartedAt: now.Add(-100 * 24 * time.Hour), FinishedAt: now.Add(-100 * 24 * time.Hour)}
	fresh := model.Run{InstanceID: "i1", AppID: 1, Mode: "check", Status: "ok",
		StartedAt: now, FinishedAt: now}
	if _, err := st.CreateRun(ctx, old); err != nil {
		t.Fatalf("create old run: %v", err)
	}
	fresh, err = st.CreateRun(ctx, fresh)
	if err != nil {
		t.Fatalf("create fresh run: %v", err)
	}

	m := New(st, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{Runs: 90 * 24 * time.Hour})
	stats := m.RunOnce(ctx)
	if stats.DeletedRuns != 1 {
		t.Fatalf("deleted_runs = %d, want 1", stats.DeletedRuns)
	}
	runs, err := st.ListRuns(ctx, model.RunFilter{InstanceID: "i1", Limit: 10})
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != fresh.ID {
		t.Fatalf("runs after prune = %+v", runs)
	}
}

func TestRunOncePreservesPendingCommandsAndPrunesAudit(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "retention-commands.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	if _, err := st.CreateInstance(ctx, model.Instance{ID: "i1", Name: "host1", TokenHash: "h2"}); err != nil {
		t.Fatalf("create instance: %v", err)
	}
	if _, err := st.CreateCommand(ctx, model.Command{InstanceID: "i1", AppID: 1, Action: model.ActionCheck}); err != nil {
		t.Fatalf("create pending command: %v", err)
	}
	done, err := st.CreateCommand(ctx, model.Command{InstanceID: "i1", AppID: 1, Action: model.ActionCheck})
	if err != nil {
		t.Fatalf("create done command: %v", err)
	}
	if err := st.CompleteCommand(ctx, "i1", done.ID, model.CmdDone, "{}", ""); err != nil {
		t.Fatalf("complete command: %v", err)
	}
	if err := st.AppendAudit(ctx, model.AuditEntry{At: time.Now().UTC(), User: "admin", Method: "POST", Path: "/x", Status: 200}); err != nil {
		t.Fatalf("append audit: %v", err)
	}

	m := New(st, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		Commands: time.Nanosecond, Audit: time.Nanosecond,
	})
	stats := m.RunOnce(ctx)
	if stats.DeletedCommands != 1 || stats.DeletedAudit != 1 {
		t.Fatalf("stats = %+v, want 1 command and 1 audit entry deleted", stats)
	}
	pending, err := st.PendingCommands(ctx, "i1")
	if err != nil {
		t.Fatalf("pending commands: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending commands after prune = %+v", pending)
	}
}
