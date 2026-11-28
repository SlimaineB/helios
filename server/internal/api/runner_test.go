package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/store"
)

func createInstance(t *testing.T, st store.Store, id, mode, project string) model.Instance {
	t.Helper()
	inst, err := st.CreateInstance(context.Background(), model.Instance{
		ID: id, Name: id, Project: project, ExecutionMode: mode, Tags: []string{},
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	return inst
}

func runnerRequest(s *Server, handler http.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	s.runnerAuth(handler)(rec, req)
	return rec
}

func TestRunnerJobLifecycle(t *testing.T) {
	s, st := newTestServer(t)
	s.SetRunnerToken("secret")
	ctx := context.Background()
	app, err := st.CreateApplication(ctx, model.Application{
		Name: "web", Project: "default", RepoURL: "https://example.test/web.git", Branch: "main",
		Path: "site.yml", Vars: "{}", ExecutionMode: model.ExecRunner, RunnerTags: []string{"eu"},
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := st.CreateRunnerJob(ctx, app.ID, model.ActionApply, "manual", app.RunnerTags)
	if err != nil {
		t.Fatal(err)
	}

	rec := runnerRequest(s, s.handleRunnerHeartbeat, http.MethodPost, "/api/v1/runner/heartbeat",
		`{"runner_id":"bastion-eu-1","tags":["eu","linux"],"version":"test"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("heartbeat: %d %s", rec.Code, rec.Body.String())
	}

	rec = runnerRequest(s, s.handleRunnerClaim, http.MethodPost, "/api/v1/runner/jobs/claim",
		`{"runner_id":"bastion-eu-1","tags":["eu","linux"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("claim: %d %s", rec.Code, rec.Body.String())
	}
	var payload runnerJobPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Job.ID != job.ID || payload.Application.ID != app.ID {
		t.Fatalf("claimed job %d app %d, want job %d app %d", payload.Job.ID, payload.Application.ID, job.ID, app.ID)
	}

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/runner/jobs/%d/result", job.ID),
		strings.NewReader(`{"runner_id":"bastion-eu-1","status":"ok","summary":{"ok":2},"started_at":"2026-01-01T00:00:00Z","finished_at":"2026-01-01T00:01:00Z"}`))
	req.SetPathValue("id", fmt.Sprint(job.ID))
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	s.runnerAuth(s.handleRunnerJobResult)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("result: %d %s", rec.Code, rec.Body.String())
	}
	var run model.Run
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if run.AppID != app.ID || run.RunnerID != "bastion-eu-1" || run.Mode != model.ActionApply || run.Status != model.JobOK {
		t.Fatalf("run = %+v", run)
	}

	jobs, err := st.ListRunnerJobs(ctx, app.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Status != model.JobOK || jobs[0].RunID != run.ID {
		t.Fatalf("jobs = %+v", jobs)
	}

	rec = runnerRequest(s, s.handleRunnerClaim, http.MethodPost, "/api/v1/runner/jobs/claim",
		`{"runner_id":"bastion-eu-1","tags":["eu","linux"]}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("second claim: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRunnerClaimRequeuesStaleJob(t *testing.T) {
	s, st := newTestServer(t)
	s.SetRunnerToken("secret")
	ctx := context.Background()
	app, err := st.CreateApplication(ctx, model.Application{
		Name: "web", Project: "default", RepoURL: "https://example.test/web.git", Branch: "main",
		Path: "site.yml", Vars: "{}", ExecutionMode: model.ExecRunner, RunnerTags: []string{"eu"},
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := st.CreateRunnerJob(ctx, app.ID, model.ActionCheck, "manual", app.RunnerTags)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := st.ClaimRunnerJob(ctx, "bastion-a", []string{"eu"}, time.Now().Add(-time.Minute))
	if err != nil || claimed.ID != job.ID {
		t.Fatalf("first claim = %+v, %v", claimed, err)
	}
	claimed, err = st.ClaimRunnerJob(ctx, "bastion-b", []string{"eu"}, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != job.ID || claimed.RunnerID != "bastion-b" {
		t.Fatalf("stale job not reclaimed: %+v", claimed)
	}
}

func TestRunnerClaimRespectsTags(t *testing.T) {
	s, st := newTestServer(t)
	s.SetRunnerToken("secret")
	ctx := context.Background()
	app, err := st.CreateApplication(ctx, model.Application{
		Name: "db", Project: "default", RepoURL: "https://example.test/db.git", Branch: "main",
		Path: "site.yml", Vars: "{}", ExecutionMode: model.ExecRunner, RunnerTags: []string{"us"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRunnerJob(ctx, app.ID, model.ActionCheck, "manual", app.RunnerTags); err != nil {
		t.Fatal(err)
	}
	rec := runnerRequest(s, s.handleRunnerClaim, http.MethodPost, "/api/v1/runner/jobs/claim",
		`{"runner_id":"bastion-eu-1","tags":["eu"]}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("tag mismatch: %d %s", rec.Code, rec.Body.String())
	}
}
