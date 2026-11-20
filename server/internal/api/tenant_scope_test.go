package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/rbac"
)

func withPrincipal(r *http.Request, p *principal) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxPrincipal, p))
}

func TestListProjectsIsTenantScoped(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	for _, name := range []string{"backend", "frontend"} {
		if _, err := st.CreateProject(ctx, model.Project{Name: name}); err != nil {
			t.Fatalf("create project: %v", err)
		}
	}
	rb, err := rbac.New(rbac.Config{
		Policies: []string{"p, role:tenant, projects, get, backend"},
		Bindings: []string{"g, alice@example.com, role:tenant"},
	})
	if err != nil {
		t.Fatalf("rbac: %v", err)
	}
	s.opAuth = &OpAuth{RBAC: rb}
	groups := []string{"alice@example.com"}
	p := &principal{Email: "alice@example.com", Groups: groups, Roles: rb.RolesForGroups(groups)}

	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil), p)
	rec := httptest.NewRecorder()
	s.handleListProjects(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var got []model.Project
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].Name != "backend" {
		t.Fatalf("projects = %+v, want only backend", got)
	}
}

func TestListRepositoriesIsTenantScoped(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	for _, name := range []string{"backend", "frontend"} {
		if _, err := st.CreateProject(ctx, model.Project{Name: name}); err != nil {
			t.Fatalf("create project: %v", err)
		}
	}
	backendRepo, err := st.CreateRepository(ctx, model.Repository{Name: "backend", URL: "http://git/backend.git", Project: "backend"})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	if _, err := st.CreateRepository(ctx, model.Repository{Name: "frontend", URL: "http://git/frontend.git", Project: "frontend"}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	if _, err := st.CreateApplication(ctx, model.Application{Name: "backend-app", RepoURL: backendRepo.URL, Project: "backend"}); err != nil {
		t.Fatalf("create app: %v", err)
	}

	rb, err := rbac.New(rbac.Config{
		Policies: []string{
			"p, role:tenant, repositories, get, backend",
			"p, role:tenant, applications, get, backend",
		},
		Bindings: []string{"g, alice@example.com, role:tenant"},
	})
	if err != nil {
		t.Fatalf("rbac: %v", err)
	}
	s.opAuth = &OpAuth{RBAC: rb}
	groups := []string{"alice@example.com"}
	p := &principal{Email: "alice@example.com", Groups: groups, Roles: rb.RolesForGroups(groups)}

	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/repositories", nil), p)
	rec := httptest.NewRecorder()
	s.handleListRepositories(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var got []repoView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].URL != backendRepo.URL {
		t.Fatalf("repositories = %+v, want only %s", got, backendRepo.URL)
	}
}

func TestRepositoriesCanShareURLAcrossProjects(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	for _, name := range []string{"backend", "frontend"} {
		if _, err := st.CreateProject(ctx, model.Project{Name: name}); err != nil {
			t.Fatalf("create project: %v", err)
		}
	}
	url := "http://git/shared.git"
	backendRepo, err := st.CreateRepository(ctx, model.Repository{Name: "backend", URL: url, Project: "backend", Password: "backend-secret"})
	if err != nil {
		t.Fatalf("create backend repo: %v", err)
	}
	frontendRepo, err := st.CreateRepository(ctx, model.Repository{Name: "frontend", URL: url, Project: "frontend", Password: "frontend-secret"})
	if err != nil {
		t.Fatalf("create frontend repo: %v", err)
	}
	if backendRepo.ID == frontendRepo.ID {
		t.Fatal("repositories in different projects must be distinct")
	}
	got, err := st.GetRepositoryByProjectURL(ctx, "frontend", url)
	if err != nil || got.Password != "frontend-secret" {
		t.Fatalf("frontend lookup = %+v err=%v", got, err)
	}
	if got, err = st.GetRepositoryByProjectURL(ctx, "backend", url); err != nil || got.Password != "backend-secret" {
		t.Fatalf("backend lookup = %+v err=%v", got, err)
	}

	rb, err := rbac.New(rbac.Config{
		Policies: []string{"p, role:tenant, repositories, get, backend"},
		Bindings: []string{"g, alice@example.com, role:tenant"},
	})
	if err != nil {
		t.Fatalf("rbac: %v", err)
	}
	s.opAuth = &OpAuth{RBAC: rb}
	groups := []string{"alice@example.com"}
	p := &principal{Email: "alice@example.com", Groups: groups, Roles: rb.RolesForGroups(groups)}
	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/repositories", nil), p)
	rec := httptest.NewRecorder()
	s.handleListRepositories(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var gotList []repoView
	if err := json.Unmarshal(rec.Body.Bytes(), &gotList); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(gotList) != 1 || gotList[0].Project != "backend" || gotList[0].PasswordSet != true {
		t.Fatalf("repositories = %+v, want only backend repo", gotList)
	}
}
