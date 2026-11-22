package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/slim/helios/server/internal/gitops"
	"github.com/slim/helios/server/internal/model"
)

func TestGitOpsManagedByAnnotations(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()

	cfg, err := st.CreateConfigRepo(ctx, model.ConfigRepo{
		Name: "platform", URL: "http://cfg/platform.git", Branch: "main",
	})
	if err != nil {
		t.Fatalf("create config repo: %v", err)
	}
	if _, err := st.CreateProject(ctx, model.Project{Name: "team"}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := st.CreateRepository(ctx, model.Repository{
		Name: "demo", URL: "http://repo/demo.git", Project: "team",
	}); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	app, err := st.CreateApplication(ctx, model.Application{
		Name: "web", RepoURL: "http://repo/demo.git", Branch: "main", Path: "site.yml",
		Vars: "{}", Project: "team",
	})
	if err != nil {
		t.Fatalf("create application: %v", err)
	}
	inst, err := st.CreateInstance(ctx, model.Instance{ID: "ins-1", Name: "web-1", TokenHash: "tok", Project: "team"})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	if err := st.Assign(ctx, inst.ID, app.ID); err != nil {
		t.Fatalf("assign: %v", err)
	}

	for _, m := range []struct{ kind, name string }{
		{gitops.KindProject, "team"},
		{gitops.KindRepository, "team::http://repo/demo.git"},
		{gitops.KindApplication, "web"},
		{gitops.KindAssignment, "ins-1::web"},
	} {
		if err := st.SetManagedResource(ctx, cfg.ID, m.kind, m.name); err != nil {
			t.Fatalf("set managed %s/%s: %v", m.kind, m.name, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications", nil)
	rec := httptest.NewRecorder()
	s.handleListApplications(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list applications: status %d body %s", rec.Code, rec.Body.String())
	}
	var apps []model.Application
	if err := json.Unmarshal(rec.Body.Bytes(), &apps); err != nil {
		t.Fatalf("decode applications: %v", err)
	}
	if len(apps) != 1 || apps[0].ManagedBy != "platform" || apps[0].ManagedByRepoID != cfg.ID {
		t.Fatalf("app managed fields = %+v", apps[0])
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	rec = httptest.NewRecorder()
	s.handleListProjects(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list projects: status %d body %s", rec.Code, rec.Body.String())
	}
	var projects []model.Project
	if err := json.Unmarshal(rec.Body.Bytes(), &projects); err != nil {
		t.Fatalf("decode projects: %v", err)
	}
	if len(projects) != 1 || projects[0].ManagedBy != "platform" {
		t.Fatalf("project managed fields = %+v", projects[0])
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/repositories", nil)
	rec = httptest.NewRecorder()
	s.handleListRepositories(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list repositories: status %d body %s", rec.Code, rec.Body.String())
	}
	var repos []repoView
	if err := json.Unmarshal(rec.Body.Bytes(), &repos); err != nil {
		t.Fatalf("decode repositories: %v", err)
	}
	if len(repos) != 1 || repos[0].ManagedBy != "platform" {
		t.Fatalf("repo managed fields = %+v", repos[0])
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/instances/ins-1", nil)
	req.SetPathValue("id", "ins-1")
	rec = httptest.NewRecorder()
	s.handleGetInstance(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get instance: status %d body %s", rec.Code, rec.Body.String())
	}
	var got InstanceWithApps
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode instance: %v", err)
	}
	if len(got.Apps) != 1 || got.Apps[0].ManagedBy != "platform" || got.Apps[0].Application.ManagedBy != "platform" {
		t.Fatalf("assignment managed fields = %+v app=%+v", got.Apps[0], got.Apps[0].Application)
	}
}
