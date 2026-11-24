package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/repocache"
)

func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", msg}} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

func TestApplicationInventoryEndpoint(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	repo, _ := testRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, "inventory"), 0o755); err != nil {
		t.Fatal(err)
	}
	hosts := "[web]\nweb-1 ansible_host=10.0.0.11 ansible_user=deploy\n\n[web:vars]\nansible_password=hunter2\n"
	if err := os.WriteFile(filepath.Join(repo, "inventory", "hosts.ini"), []byte(hosts), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, repo, "inventory")

	app, err := st.CreateApplication(ctx, model.Application{
		Name: "demo", RepoURL: repo, Branch: "main", Path: "site.yml",
		InventoryPath: "inventory", Vars: "{}",
	})
	if err != nil {
		t.Fatal(err)
	}
	cache, err := repocache.New(repocache.Config{Dir: t.TempDir()}, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.SetRepoCache(cache)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications/1/inventory", nil)
	req.SetPathValue("appId", strconv.FormatInt(app.ID, 10))
	rec := httptest.NewRecorder()
	s.handleApplicationInventory(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("inventory: status %d body %s", rec.Code, rec.Body.String())
	}
	var out applicationInventory
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Enabled || len(out.Groups) != 1 {
		t.Fatalf("inventory = %+v", out)
	}
	group := out.Groups[0]
	if group.Name != "web" || len(group.Hosts) != 1 || group.Hosts[0].Name != "web-1" {
		t.Fatalf("group = %+v", group)
	}
	if got := group.Hosts[0].Vars["ansible_host"]; got != "10.0.0.11" {
		t.Fatalf("ansible_host = %v", got)
	}
	if got := group.Vars["ansible_password"]; got != redactedValue {
		t.Fatalf("password not redacted: %v", got)
	}
}

func TestApplicationInventoryWithoutPath(t *testing.T) {
	s, st := newTestServer(t)
	app, err := st.CreateApplication(context.Background(), model.Application{
		Name: "demo", RepoURL: "http://repo/demo.git", Branch: "main", Path: "site.yml", Vars: "{}",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications/1/inventory", nil)
	req.SetPathValue("appId", strconv.FormatInt(app.ID, 10))
	rec := httptest.NewRecorder()
	s.handleApplicationInventory(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var out applicationInventory
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Warnings) == 0 || out.Enabled {
		t.Fatalf("inventory = %+v", out)
	}
}
