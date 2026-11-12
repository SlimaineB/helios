package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/repocache"
)

func testRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "-C", dir, "init", "-b", "main")
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "site.yml"), []byte("- hosts: all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "first"}} {
		cmd = exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return dir, strings.TrimSpace(string(out))
}

func TestAgentSnapshotEndpoint(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	inst := createInstance(t, st, "i1", model.ModeAgent, 0)
	repo, commit := testRepo(t)
	app, err := st.CreateApplication(ctx, model.Application{
		Name: "demo", RepoURL: repo, Branch: "main", Path: "site.yml", Vars: "{}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Assign(ctx, inst.ID, app.ID); err != nil {
		t.Fatal(err)
	}
	cache, err := repocache.New(repocache.Config{Dir: t.TempDir()}, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.SetRepoCache(cache)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/apps/1/snapshot", nil)
	req.SetPathValue("appId", "1")
	req = req.WithContext(context.WithValue(req.Context(), ctxInstance, inst))
	rec := httptest.NewRecorder()
	s.handleAgentSnapshot(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("snapshot: status %d body %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Helios-Commit"); got != commit {
		t.Fatalf("commit header = %q, want %q", got, commit)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("empty snapshot body")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/agent/apps/1/snapshot", nil)
	req.SetPathValue("appId", "1")
	req.Header.Set("If-None-Match", `"`+commit+`"`)
	req = req.WithContext(context.WithValue(req.Context(), ctxInstance, inst))
	rec = httptest.NewRecorder()
	s.handleAgentSnapshot(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("current snapshot: status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestAgentSnapshotRequiresAssignment(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	inst := createInstance(t, st, "i1", model.ModeAgent, 0)
	repo, _ := testRepo(t)
	if _, err := st.CreateApplication(ctx, model.Application{
		Name: "demo", RepoURL: repo, Branch: "main", Path: "site.yml", Vars: "{}",
	}); err != nil {
		t.Fatal(err)
	}
	cache, err := repocache.New(repocache.Config{Dir: t.TempDir()}, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.SetRepoCache(cache)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/apps/1/snapshot", nil)
	req.SetPathValue("appId", "1")
	req = req.WithContext(context.WithValue(req.Context(), ctxInstance, inst))
	rec := httptest.NewRecorder()
	s.handleAgentSnapshot(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unassigned: status %d body %s", rec.Code, rec.Body.String())
	}
}
