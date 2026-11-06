package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/store"
)

func newTestServer(t *testing.T) (*Server, store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return New(st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, false), st
}

func TestUpdateApplicationPreservesOmittedFields(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	app, err := st.CreateApplication(ctx, model.Application{
		Name: "demo", RepoURL: "http://repo/demo.git", Branch: "main", Path: "site.yml",
		Vars: `{"x":"1"}`, AutoHeal: true, SyncInterval: 120,
	})
	if err != nil {
		t.Fatalf("create app: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/applications/1", strings.NewReader(`{"name":"renamed"}`))
	req.SetPathValue("appId", "1")
	rec := httptest.NewRecorder()
	s.handleUpdateApplication(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update app: status %d body %s", rec.Code, rec.Body.String())
	}

	got, err := st.GetApplication(ctx, app.ID)
	if err != nil {
		t.Fatalf("get app: %v", err)
	}
	if got.Name != "renamed" {
		t.Fatalf("name = %q, want renamed", got.Name)
	}
	if got.Vars != `{"x":"1"}` {
		t.Fatalf("vars = %q, want preserved", got.Vars)
	}
	if !got.AutoHeal {
		t.Fatalf("autoheal = false, want preserved true")
	}
	if got.SyncInterval != 120 {
		t.Fatalf("sync_interval = %d, want preserved 120", got.SyncInterval)
	}
}

func TestUpdateApplicationCanFlipAutoHeal(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	app, err := st.CreateApplication(ctx, model.Application{
		Name: "demo", RepoURL: "http://repo/demo.git", Branch: "main", Path: "site.yml",
		Vars: "{}", AutoHeal: true,
	})
	if err != nil {
		t.Fatalf("create app: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/applications/1", strings.NewReader(`{"autoheal":false}`))
	req.SetPathValue("appId", "1")
	rec := httptest.NewRecorder()
	s.handleUpdateApplication(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update app: status %d body %s", rec.Code, rec.Body.String())
	}

	got, err := st.GetApplication(ctx, app.ID)
	if err != nil {
		t.Fatalf("get app: %v", err)
	}
	if got.AutoHeal {
		t.Fatalf("autoheal = true, want false")
	}
}

func TestUpdateRepositoryPreservesUsernameAndCreatedAt(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	repo, err := st.CreateRepository(ctx, model.Repository{
		Name: "demo", URL: "http://repo/demo.git", Username: "bot", Password: "secret",
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/repositories/1", strings.NewReader(`{"url":"http://repo/demo.git"}`))
	req.SetPathValue("repoId", "1")
	rec := httptest.NewRecorder()
	s.handleUpdateRepository(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update repo: status %d body %s", rec.Code, rec.Body.String())
	}

	got, err := st.GetRepository(ctx, repo.ID)
	if err != nil {
		t.Fatalf("get repo: %v", err)
	}
	if got.Username != "bot" {
		t.Fatalf("username = %q, want bot", got.Username)
	}
	if got.Password != "secret" {
		t.Fatalf("password was not preserved")
	}
	if got.CreatedAt.IsZero() {
		t.Fatalf("created_at was not preserved")
	}
}

func TestStatusRejectsInvalidStatus(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/status", strings.NewReader(`{"app_id":1,"mode":"check","status":"bogus"}`))
	req = req.WithContext(context.WithValue(req.Context(), ctxInstance, model.Instance{ID: "ins-test"}))
	rec := httptest.NewRecorder()
	s.handleStatus(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHealthFailsWhenDatabaseClosed(t *testing.T) {
	s, st := newTestServer(t)
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	rec := httptest.NewRecorder()
	s.handleHealth(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("health = %d, want 503", rec.Code)
	}
}

func TestUpdateApplicationCanSetSyncInterval(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	app, err := st.CreateApplication(ctx, model.Application{
		Name: "demo", RepoURL: "http://repo/demo.git", Branch: "main", Path: "site.yml",
		Vars: "{}",
	})
	if err != nil {
		t.Fatalf("create app: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/applications/1", strings.NewReader(`{"sync_interval":45}`))
	req.SetPathValue("appId", "1")
	rec := httptest.NewRecorder()
	s.handleUpdateApplication(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update app: status %d body %s", rec.Code, rec.Body.String())
	}
	got, err := st.GetApplication(ctx, app.ID)
	if err != nil {
		t.Fatalf("get app: %v", err)
	}
	if got.SyncInterval != 45 {
		t.Fatalf("sync_interval = %d, want 45", got.SyncInterval)
	}
}

func TestCreateApplicationValidatesSyncInterval(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	body := `{"name":"demo","repo_url":"http://repo/demo.git","sync_interval":5}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/applications", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleCreateApplication(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for sync_interval=5", rec.Code)
	}

	body = `{"name":"demo","repo_url":"http://repo/demo.git","sync_interval":60}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/applications", strings.NewReader(body))
	rec = httptest.NewRecorder()
	s.handleCreateApplication(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body %s, want 201", rec.Code, rec.Body.String())
	}
	apps, err := st.ListApplications(ctx)
	if err != nil || len(apps) != 1 {
		t.Fatalf("list apps: %v n=%d", err, len(apps))
	}
	if apps[0].SyncInterval != 60 {
		t.Fatalf("sync_interval = %d, want 60", apps[0].SyncInterval)
	}
}

func TestCreateApplicationDuplicateReturnsConflict(t *testing.T) {
	s, _ := newTestServer(t)
	body := `{"name":"dup","repo_url":"http://repo/dup.git"}`
	for i, want := range []int{http.StatusCreated, http.StatusConflict} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/applications", strings.NewReader(body))
		rec := httptest.NewRecorder()
		s.handleCreateApplication(rec, req)
		if rec.Code != want {
			t.Fatalf("attempt %d: status = %d body %s, want %d", i+1, rec.Code, rec.Body.String(), want)
		}
	}
}

func TestSyncWindowFreezesApplyButNotCheck(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	app, _ := st.CreateApplication(ctx, model.Application{
		Name: "demo", RepoURL: "http://repo/demo.git", Branch: "main", Path: "site.yml", Vars: "{}", AutoHeal: true,
	})
	if _, err := st.CreateInstance(ctx, model.Instance{ID: "ins-1", Name: "one", TokenHash: "h"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Assign(ctx, "ins-1", app.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSyncWindow(ctx, model.SyncWindow{
		Name: "always", Kind: "deny", Timezone: "UTC", Days: "", StartMin: 0, EndMin: 1439, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/ins-1/apply", strings.NewReader(`{"app_id":1}`))
	req.SetPathValue("id", "ins-1")
	req.SetPathValue("action", "apply")
	rec := httptest.NewRecorder()
	s.handleCreateInstanceCommand(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("apply status = %d body %s, want 409", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/instances/ins-1/check", strings.NewReader(`{"app_id":1}`))
	req.SetPathValue("id", "ins-1")
	req.SetPathValue("action", "check")
	rec = httptest.NewRecorder()
	s.handleCreateInstanceCommand(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("check status = %d body %s, want 201", rec.Code, rec.Body.String())
	}
}

func TestProjectsScopeRepositoriesAndAssignments(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	if _, err := st.CreateProject(ctx, model.Project{Name: "finance", RepoPatterns: "https://git.example.com/finance/*"}); err != nil {
		t.Fatal(err)
	}

	body := `{"name":"pay","repo_url":"https://git.example.com/other/pay.git","project":"finance"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/applications", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleCreateApplication(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("disallowed repo status = %d body %s, want 400", rec.Code, rec.Body.String())
	}

	body = `{"name":"pay","repo_url":"https://git.example.com/finance/pay.git","project":"finance"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/applications", strings.NewReader(body))
	rec = httptest.NewRecorder()
	s.handleCreateApplication(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("allowed repo status = %d body %s, want 201", rec.Code, rec.Body.String())
	}

	if _, err := st.CreateInstance(ctx, model.Instance{ID: "ins-global", Name: "g", TokenHash: "h1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateInstance(ctx, model.Instance{ID: "ins-fin", Name: "f", TokenHash: "h2", Project: "finance"}); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/instances/ins-global/assign", strings.NewReader(`{"app_id":1}`))
	req.SetPathValue("id", "ins-global")
	rec = httptest.NewRecorder()
	s.handleAssign(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("cross-project assign status = %d body %s, want 409", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/instances/ins-fin/assign", strings.NewReader(`{"app_id":1}`))
	req.SetPathValue("id", "ins-fin")
	rec = httptest.NewRecorder()
	s.handleAssign(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("same-project assign status = %d body %s, want 200", rec.Code, rec.Body.String())
	}
}

func TestRollbackPinsRevisionAndQueuesApply(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	app, _ := st.CreateApplication(ctx, model.Application{Name: "demo", RepoURL: "http://repo/demo.git", Branch: "main", Path: "site.yml", Vars: "{}"})
	if _, err := st.CreateInstance(ctx, model.Instance{ID: "ins-1", Name: "one", TokenHash: "h"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Assign(ctx, "ins-1", app.ID); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/instances/ins-1/assign/1/revision", strings.NewReader(`{"revision":"deadbeef"}`))
	req.SetPathValue("id", "ins-1")
	req.SetPathValue("appId", "1")
	rec := httptest.NewRecorder()
	s.handleSetRevision(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rollback status = %d body %s", rec.Code, rec.Body.String())
	}
	a, err := st.GetAssignment(ctx, "ins-1", app.ID)
	if err != nil || a.TargetRevision != "deadbeef" {
		t.Fatalf("target revision = %q err=%v", a.TargetRevision, err)
	}
	cmds, err := st.PendingCommands(ctx, "ins-1")
	if err != nil || len(cmds) != 1 || cmds[0].Action != model.ActionApply {
		t.Fatalf("pending commands = %+v err=%v", cmds, err)
	}
}

func TestDeleteProjectBlockedWhenInUse(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	project, err := st.CreateProject(ctx, model.Project{Name: "finance"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	app, err := st.CreateApplication(ctx, model.Application{
		Name: "pay", RepoURL: "https://git.example.com/finance/pay.git",
		Branch: "main", Path: "site.yml", Vars: "{}", Project: "finance",
	})
	if err != nil {
		t.Fatalf("create app: %v", err)
	}
	if _, err := st.CreateInstance(ctx, model.Instance{
		ID: "ins-fin", Name: "fin", TokenHash: "h-fin", Project: "finance",
	}); err != nil {
		t.Fatalf("create instance: %v", err)
	}

	deleteProject := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/"+strconv.FormatInt(project.ID, 10), nil)
		req.SetPathValue("projectId", strconv.FormatInt(project.ID, 10))
		rec := httptest.NewRecorder()
		s.handleDeleteProject(rec, req)
		return rec
	}

	if rec := deleteProject(); rec.Code != http.StatusConflict {
		t.Fatalf("delete project with app/instance: status %d body %s, want 409", rec.Code, rec.Body.String())
	}
	if _, err := st.GetProjectByID(ctx, project.ID); err != nil {
		t.Fatalf("project was deleted while in use: %v", err)
	}

	if err := st.DeleteApplication(ctx, app.ID); err != nil {
		t.Fatalf("delete app: %v", err)
	}
	if rec := deleteProject(); rec.Code != http.StatusConflict {
		t.Fatalf("delete project with instance: status %d body %s, want 409", rec.Code, rec.Body.String())
	}

	if err := st.DeleteInstance(ctx, "ins-fin"); err != nil {
		t.Fatalf("delete instance: %v", err)
	}
	if rec := deleteProject(); rec.Code != http.StatusOK {
		t.Fatalf("delete unused project: status %d body %s, want 200", rec.Code, rec.Body.String())
	}
}

func TestDeleteRepositoryBlockedWhenInUse(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	repo, err := st.CreateRepository(ctx, model.Repository{
		Name: "demo", URL: "https://git.example.com/demo.git",
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	app, err := st.CreateApplication(ctx, model.Application{
		Name: "demo", RepoURL: repo.URL, Branch: "main", Path: "site.yml", Vars: "{}",
	})
	if err != nil {
		t.Fatalf("create app: %v", err)
	}

	deleteRepo := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/repositories/"+strconv.FormatInt(repo.ID, 10), nil)
		req.SetPathValue("repoId", strconv.FormatInt(repo.ID, 10))
		rec := httptest.NewRecorder()
		s.handleDeleteRepository(rec, req)
		return rec
	}

	if rec := deleteRepo(); rec.Code != http.StatusConflict {
		t.Fatalf("delete repo in use: status %d body %s, want 409", rec.Code, rec.Body.String())
	}
	if _, err := st.GetRepository(ctx, repo.ID); err != nil {
		t.Fatalf("repository was deleted while in use: %v", err)
	}

	if err := st.DeleteApplication(ctx, app.ID); err != nil {
		t.Fatalf("delete app: %v", err)
	}
	if rec := deleteRepo(); rec.Code != http.StatusOK {
		t.Fatalf("delete unused repo: status %d body %s, want 200", rec.Code, rec.Body.String())
	}
}

func TestAuditLogRecordsMutations(t *testing.T) {
	s, st := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", strings.NewReader(`{"name":"finance"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create project status = %d body %s", rec.Code, rec.Body.String())
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		entries, total, err := st.ListAudit(context.Background(), 10, 0)
		if err == nil && total > 0 {
			if entries[0].Method != http.MethodPost || entries[0].Path != "/api/v1/projects" || entries[0].Status != http.StatusCreated {
				t.Fatalf("audit entry = %+v", entries[0])
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("audit entry was not written")
}
