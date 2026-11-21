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
	"github.com/slim/helios/server/internal/reconciler"
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
		entries, total, err := st.ListAudit(context.Background(), model.AuditFilter{Limit: 10})
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

func TestAuditFiltersAndFacets(t *testing.T) {
	_, st := newTestServer(t)
	ctx := context.Background()
	entries := []model.AuditEntry{
		{User: "alice", Method: http.MethodPost, Path: "/api/v1/projects", Status: http.StatusCreated, RemoteIP: "10.0.0.1"},
		{User: "bob", Method: http.MethodDelete, Path: "/api/v1/projects/1", Status: http.StatusForbidden, RemoteIP: "10.0.0.2"},
		{User: "alice", Method: http.MethodPost, Path: "/api/v1/applications", Status: http.StatusInternalServerError, RemoteIP: "10.0.0.1"},
	}
	for _, e := range entries {
		if err := st.AppendAudit(ctx, e); err != nil {
			t.Fatalf("append audit: %v", err)
		}
	}

	tests := []struct {
		name   string
		filter model.AuditFilter
		want   int
	}{
		{name: "user", filter: model.AuditFilter{User: "alice"}, want: 2},
		{name: "method", filter: model.AuditFilter{Method: "delete"}, want: 1},
		{name: "status 4xx", filter: model.AuditFilter{Status: "4xx"}, want: 1},
		{name: "status 5xx", filter: model.AuditFilter{Status: "5xx"}, want: 1},
		{name: "status error", filter: model.AuditFilter{Status: "error"}, want: 2},
		{name: "path search", filter: model.AuditFilter{Query: "applications"}, want: 1},
		{name: "user search", filter: model.AuditFilter{Query: "alice"}, want: 2},
		{name: "ip search", filter: model.AuditFilter{Query: "10.0.0.2"}, want: 1},
		{name: "since", filter: model.AuditFilter{Since: time.Now().Add(-time.Minute)}, want: 3},
		{name: "until", filter: model.AuditFilter{Until: time.Now().Add(time.Minute)}, want: 3},
		{name: "future since", filter: model.AuditFilter{Since: time.Now().Add(time.Hour)}, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.filter.Limit = 10
			got, total, err := st.ListAudit(ctx, tt.filter)
			if err != nil {
				t.Fatalf("list audit: %v", err)
			}
			if total != tt.want || len(got) != tt.want {
				t.Fatalf("total=%d len=%d want %d", total, len(got), tt.want)
			}
		})
	}

	facets, err := st.AuditFacets(ctx)
	if err != nil {
		t.Fatalf("audit facets: %v", err)
	}
	if strings.Join(facets.Users, ",") != "alice,bob" {
		t.Fatalf("users = %v", facets.Users)
	}
	if strings.Join(facets.Methods, ",") != "DELETE,POST" {
		t.Fatalf("methods = %v", facets.Methods)
	}
}

func TestConfigRepoCRUD(t *testing.T) {
	s, st := newTestServer(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config-repos",
		strings.NewReader(`{"name":"cfg","url":"https://example.com/config.git"}`))
	s.handleCreateConfigRepo(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create config repo: status %d body %s", rec.Code, rec.Body.String())
	}
	repo, err := st.GetConfigRepo(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !repo.Enabled || repo.Branch != "main" || repo.PollInterval != 300 {
		t.Fatalf("unexpected defaults: %+v", repo)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/v1/config-repos/1", strings.NewReader(`{"poll_interval":60,"enabled":false}`))
	req.SetPathValue("repoId", "1")
	s.handleUpdateConfigRepo(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update config repo: status %d body %s", rec.Code, rec.Body.String())
	}
	got, err := st.GetConfigRepo(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "cfg" || got.URL != "https://example.com/config.git" {
		t.Fatalf("update cleared fields: %+v", got)
	}
	if got.Enabled || got.PollInterval != 60 {
		t.Fatalf("update not applied: %+v", got)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/config-repos", nil)
	s.handleListConfigRepos(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list config repos: status %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/config-repos/1", nil)
	req.SetPathValue("repoId", "1")
	s.handleDeleteConfigRepo(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete config repo: status %d body %s", rec.Code, rec.Body.String())
	}
	if _, err := st.GetConfigRepo(context.Background(), 1); err == nil {
		t.Fatal("config repo still present after delete")
	}
}

func TestSyncConfigRepoReturnsConflictWhenLeaseHeld(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	repo, err := st.CreateConfigRepo(ctx, model.ConfigRepo{
		Name: "cfg", URL: "https://example.com/config.git", Branch: "main", PollInterval: 300, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create config repo: %v", err)
	}
	if ok, err := st.AcquireLease(ctx, "gitops:reconcile", "other-replica", time.Minute); err != nil || !ok {
		t.Fatalf("seed lease: ok=%v err=%v", ok, err)
	}
	s.SetReconciler(reconciler.New(st, s.log, t.TempDir()))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config-repos/1/sync", nil)
	req.SetPathValue("repoId", strconv.FormatInt(repo.ID, 10))
	s.handleSyncConfigRepo(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("sync while leased: status %d body %s", rec.Code, rec.Body.String())
	}
}
