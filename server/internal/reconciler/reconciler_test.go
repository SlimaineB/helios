package reconciler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/store"
)

func TestSyncReconcilesAndPrunesManagedObjects(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "helios.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, err := st.CreateInstance(ctx, model.Instance{
		ID: "ins-1", Name: "host-1", TokenHash: "hash-1", Project: "team",
	}); err != nil {
		t.Fatal(err)
	}
	manual, err := st.CreateApplication(ctx, model.Application{
		Name: "manual-app", RepoURL: "https://example.com/manual.git", Branch: "main", Path: "site.yml", Vars: "{}",
	})
	if err != nil {
		t.Fatal(err)
	}

	repoDir := initRepo(t)
	writeManifests(t, repoDir, `
apiVersion: helios.dev/v1alpha1
kind: Project
metadata:
  name: team
spec:
  repoPatterns: "https://example.com/*"
---
apiVersion: helios.dev/v1alpha1
kind: Repository
metadata:
  name: app-repo
spec:
  url: https://example.com/app.git
---
apiVersion: helios.dev/v1alpha1
kind: Application
metadata:
  name: demo
  project: team
spec:
  repoRef: app-repo
  branch: main
  path: site.yml
  vars:
    env: prod
    replicas: 2
  autoHeal: true
---
apiVersion: helios.dev/v1alpha1
kind: Assignment
metadata:
  name: demo-on-host
spec:
  application: demo
  instance: ins-1
  autoHeal: false
`)
	commit(t, repoDir, "initial manifests")

	cfg, err := st.CreateConfigRepo(ctx, model.ConfigRepo{
		Name: "cfg", URL: repoDir, Branch: "main", PollInterval: 300, Enabled: true, Prune: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := New(st, slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir())
	if err := r.Sync(ctx, cfg.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := st.GetProjectByName(ctx, "team"); err != nil {
		t.Fatalf("project not created: %v", err)
	}
	if _, err := st.GetRepositoryByURL(ctx, "https://example.com/app.git"); err != nil {
		t.Fatalf("repository not created: %v", err)
	}
	app, err := st.GetApplicationByName(ctx, "demo")
	if err != nil {
		t.Fatalf("application not created: %v", err)
	}
	if !app.AutoHeal || app.Project != "team" {
		t.Fatalf("unexpected app: %+v", app)
	}
	if app.Vars != `{"env":"prod","replicas":2}` {
		t.Fatalf("unexpected app vars: %s", app.Vars)
	}
	if _, err := st.GetAssignment(ctx, "ins-1", app.ID); err != nil {
		t.Fatalf("assignment not created: %v", err)
	}
	if _, err := st.GetApplication(ctx, manual.ID); err != nil {
		t.Fatalf("manual application was touched: %v", err)
	}

	// Remove the assignment and application from Git. Prune should remove the
	// assignment first, then the app, while leaving the manual app alone.
	writeManifests(t, repoDir, `
apiVersion: helios.dev/v1alpha1
kind: Project
metadata:
  name: team
spec:
  repoPatterns: "https://example.com/*"
---
apiVersion: helios.dev/v1alpha1
kind: Repository
metadata:
  name: app-repo
spec:
  url: https://example.com/app.git
`)
	commit(t, repoDir, "remove app")
	if err := r.Sync(ctx, cfg.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetApplicationByName(ctx, "demo"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("demo app should be pruned, got %v", err)
	}
	if _, err := st.GetAssignment(ctx, "ins-1", app.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("assignment should be pruned, got %v", err)
	}
	if _, err := st.GetApplication(ctx, manual.ID); err != nil {
		t.Fatalf("manual application should remain: %v", err)
	}

	got, err := st.GetConfigRepo(ctx, cfg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastCommit == "" || got.LastSummary["deleted"] == 0 {
		t.Fatalf("status not updated: %+v", got)
	}
}

func TestSyncSkipsWhenAnotherReplicaHoldsLease(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "helios.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg, err := st.CreateConfigRepo(ctx, model.ConfigRepo{
		Name: "cfg", URL: "https://example.com/config.git", Branch: "main", PollInterval: 300, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := st.AcquireLease(ctx, gitopsLease, "other-replica", time.Minute); err != nil || !ok {
		t.Fatalf("seed lease: ok=%v err=%v", ok, err)
	}
	r := New(st, slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir())
	if err := r.Sync(ctx, cfg.ID); !errors.Is(err, ErrSyncInProgress) {
		t.Fatalf("Sync = %v, want ErrSyncInProgress", err)
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	return dir
}

func writeManifests(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "helios.yaml"), []byte(strings.TrimSpace(body)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, dir, msg string) {
	t.Helper()
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-m", msg)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}
