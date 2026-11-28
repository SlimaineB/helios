package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/slim/helios/server/internal/model"
)

func TestListInstancesFiltersPaginatesAndHydrates(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	now := time.Now().UTC()
	app, err := st.CreateApplication(ctx, model.Application{Name: "web", RepoURL: "https://example.com/web.git", Branch: "main", Path: "site.yml", Vars: "{}"})
	if err != nil {
		t.Fatalf("create app: %v", err)
	}
	for i, spec := range []struct {
		id, project string
		tags        []string
		mode        string
		seen        time.Time
		state, run  string
	}{
		{"web-1", "prod", []string{"prod", "team-a"}, model.ExecAgent, now, model.StateDrift, model.RunDrift},
		{"web-2", "", []string{"prod"}, model.ExecAgent, now.Add(-time.Hour), model.StateOK, model.RunOK},
		{"web-3", "staging", []string{"staging"}, model.ExecAgent, now, model.StateUnknown, ""},
	} {
		inst, err := st.CreateInstance(ctx, model.Instance{ID: spec.id, Name: spec.id, TokenHash: "tok-" + spec.id, Project: spec.project, Tags: spec.tags, ExecutionMode: spec.mode})
		if err != nil {
			t.Fatalf("create %s: %v", spec.id, err)
		}
		_ = inst
		if err := st.TouchInstance(ctx, spec.id, "v1", spec.id, "linux", 30, spec.seen); err != nil {
			t.Fatalf("touch %s: %v", spec.id, err)
		}
		if err := st.UpdateInstanceState(ctx, spec.id, spec.state, ""); err != nil {
			t.Fatalf("state %s: %v", spec.id, err)
		}
		if err := st.Assign(ctx, spec.id, app.ID); err != nil {
			t.Fatalf("assign %s: %v", spec.id, err)
		}
		if spec.run != "" {
			if _, err := st.CreateRun(ctx, model.Run{InstanceID: spec.id, AppID: app.ID, Mode: "check", Status: spec.run, StartedAt: now, FinishedAt: now}); err != nil {
				t.Fatalf("run %s: %v", spec.id, err)
			}
		}
		if i == 2 {
			break
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances?tag=PROD&limit=1&facets=1&stats=1", nil)
	rec := httptest.NewRecorder()
	s.handleListInstances(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status %d body %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Items  []InstanceWithApps      `json:"items"`
		Total  int                     `json:"total"`
		Limit  int                     `json:"limit"`
		Offset int                     `json:"offset"`
		Facets model.InstanceFacets    `json:"facets"`
		Stats  model.InstanceListStats `json:"stats"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Total != 2 || got.Limit != 1 || len(got.Items) != 1 || got.Items[0].ID != "web-1" {
		t.Fatalf("page = %+v", got)
	}
	if len(got.Items[0].Apps) != 1 || got.Items[0].Apps[0].Application.ID != app.ID || !got.Items[0].Apps[0].HasCheck || got.Items[0].Apps[0].LatestCheck.Status != model.RunDrift {
		t.Fatalf("hydrated apps = %+v", got.Items[0].Apps)
	}
	if got.Stats.Total != 2 || got.Stats.Drift != 1 || got.Stats.Synced != 1 {
		t.Fatalf("stats = %+v", got.Stats)
	}
	if len(got.Facets.Tags) != 3 || len(got.Facets.Apps) != 1 {
		t.Fatalf("facets = %+v", got.Facets)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/instances?project=__global__&facets=1&stats=1", nil)
	rec = httptest.NewRecorder()
	s.handleListInstances(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode global: %v", err)
	}
	if got.Total != 1 || len(got.Items) != 1 || got.Items[0].ID != "web-2" {
		t.Fatalf("global page = %+v", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/instances?online=1&facets=1&stats=1", nil)
	rec = httptest.NewRecorder()
	s.handleListInstances(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode online: %v", err)
	}
	if got.Total != 2 || got.Stats.Offline != 0 {
		t.Fatalf("online page = %+v", got)
	}
}
