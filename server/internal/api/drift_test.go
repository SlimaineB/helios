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

func TestDriftApplicationStatsAndCompactInstances(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	now := time.Now().UTC()
	app, err := st.CreateApplication(ctx, model.Application{Name: "web", RepoURL: "https://example.com/web.git", Branch: "main", Path: "site.yml", Vars: "{}"})
	if err != nil {
		t.Fatalf("create app: %v", err)
	}
	for i, spec := range []struct {
		id, status string
		stuck      bool
		assign     bool
	}{
		{"drift-1", model.RunDrift, false, true},
		{"drift-2", model.RunOK, false, true},
		{"drift-3", model.RunOK, true, true},
		{"drift-free", "", false, false},
	} {
		if _, err := st.CreateInstance(ctx, model.Instance{ID: spec.id, Name: spec.id, TokenHash: "tok-" + spec.id}); err != nil {
			t.Fatalf("create %s: %v", spec.id, err)
		}
		if err := st.TouchInstance(ctx, spec.id, "v1", spec.id, "linux", 30, now, false); err != nil {
			t.Fatalf("touch %s: %v", spec.id, err)
		}
		if err := st.UpdateInstanceState(ctx, spec.id, model.StateDrift, ""); err != nil {
			t.Fatalf("state %s: %v", spec.id, err)
		}
		if !spec.assign {
			continue
		}
		if err := st.Assign(ctx, spec.id, app.ID); err != nil {
			t.Fatalf("assign %s: %v", spec.id, err)
		}
		if spec.status != "" {
			if _, err := st.CreateRun(ctx, model.Run{InstanceID: spec.id, AppID: app.ID, Mode: "check", Status: spec.status, StartedAt: now.Add(-time.Duration(i) * time.Minute), FinishedAt: now.Add(-time.Duration(i) * time.Minute)}); err != nil {
				t.Fatalf("run %s: %v", spec.id, err)
			}
		}
		if spec.stuck {
			if err := st.SetStuck(ctx, spec.id, app.ID, true); err != nil {
				t.Fatalf("stuck %s: %v", spec.id, err)
			}
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/drift?limit=1&facets=1", nil)
	rec := httptest.NewRecorder()
	s.handleListDrift(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("drift: status %d body %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Items  []DriftRow             `json:"items"`
		Total  int                    `json:"total"`
		Limit  int                    `json:"limit"`
		Offset int                    `json:"offset"`
		Stats  model.AssignmentStats  `json:"stats"`
		Facets model.AssignmentFacets `json:"facets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode drift: %v", err)
	}
	if got.Total != 2 || got.Limit != 1 || len(got.Items) != 1 || got.Items[0].App.Application.ID != app.ID {
		t.Fatalf("drift page: %+v", got)
	}
	if got.Stats.Total != 2 || got.Stats.Instances != 2 || got.Stats.Apps != 1 {
		t.Fatalf("drift stats: %+v", got.Stats)
	}
	if len(got.Facets.Apps) != 1 || len(got.Facets.Projects) != 1 {
		t.Fatalf("drift facets: %+v", got.Facets)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/application-stats", nil)
	rec = httptest.NewRecorder()
	s.handleApplicationStats(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("app stats: status %d body %s", rec.Code, rec.Body.String())
	}
	var appStats struct {
		Items []model.ApplicationStats `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &appStats); err != nil {
		t.Fatalf("decode app stats: %v", err)
	}
	if len(appStats.Items) != 1 || appStats.Items[0].Instances != 3 || appStats.Items[0].Drift != 1 || appStats.Items[0].Stuck != 1 || appStats.Items[0].Synced != 2 {
		t.Fatalf("app stats: %+v", appStats.Items)
	}

	for _, tc := range []struct {
		query string
		want  int
	}{
		{"compact=1", 4},
		{"compact=1&app_ids=1", 3},
		{"compact=1&repo=https%3A%2F%2Fexample.com%2Fweb.git", 3},
	} {
		req = httptest.NewRequest(http.MethodGet, "/api/v1/instances?"+tc.query, nil)
		rec = httptest.NewRecorder()
		s.handleListInstances(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("instances %s: status %d body %s", tc.query, rec.Code, rec.Body.String())
		}
		var page struct {
			Items []InstanceWithApps `json:"items"`
			Total int                `json:"total"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode %s: %v", tc.query, err)
		}
		if page.Total != tc.want {
			t.Fatalf("%s total=%d want %d", tc.query, page.Total, tc.want)
		}
		for _, item := range page.Items {
			if len(item.Apps) != 0 {
				t.Fatalf("%s compact apps = %+v", tc.query, item.Apps)
			}
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/instances?compact=1&unassigned=1", nil)
	rec = httptest.NewRecorder()
	s.handleListInstances(rec, req)
	var page struct {
		Items []InstanceWithApps `json:"items"`
		Total int                `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode unassigned: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != "drift-free" {
		t.Fatalf("unassigned page: %+v", page)
	}
}
