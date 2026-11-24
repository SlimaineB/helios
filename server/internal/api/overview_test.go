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

func TestOverviewAggregatesWithoutListingWholeFleet(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	now := time.Now().UTC()
	app, err := st.CreateApplication(ctx, model.Application{Name: "web", RepoURL: "https://example.com/web.git", Branch: "main", Path: "site.yml", Vars: "{}"})
	if err != nil {
		t.Fatalf("create app: %v", err)
	}
	specs := []struct {
		id, version, state string
		seen               time.Time
		run                string
		assign, stuck      bool
	}{
		{"ov-1", "v1", model.StateDrift, now, model.RunDrift, true, false},
		{"ov-2", "v2", model.StateError, now, model.RunFailed, true, false},
		{"ov-3", "v1", model.StateOK, now, model.RunOK, true, true},
		{"ov-4", "v1", model.StateDrift, now.Add(-2 * time.Hour), model.RunDrift, true, false},
		{"ov-5", "v1", model.StateUnknown, now, "", false, false},
	}
	for _, spec := range specs {
		if _, err := st.CreateInstance(ctx, model.Instance{ID: spec.id, Name: spec.id, TokenHash: "tok-" + spec.id}); err != nil {
			t.Fatalf("create %s: %v", spec.id, err)
		}
		if err := st.TouchInstance(ctx, spec.id, spec.version, spec.id, "linux", 30, spec.seen); err != nil {
			t.Fatalf("touch %s: %v", spec.id, err)
		}
		if err := st.UpdateInstanceState(ctx, spec.id, spec.state, ""); err != nil {
			t.Fatalf("state %s: %v", spec.id, err)
		}
		if !spec.assign {
			continue
		}
		if err := st.Assign(ctx, spec.id, app.ID); err != nil {
			t.Fatalf("assign %s: %v", spec.id, err)
		}
		if spec.run != "" {
			if _, err := st.CreateRun(ctx, model.Run{InstanceID: spec.id, AppID: app.ID, Mode: "check", Status: spec.run, StartedAt: now, FinishedAt: now}); err != nil {
				t.Fatalf("run %s: %v", spec.id, err)
			}
		}
		if spec.stuck {
			if err := st.SetStuck(ctx, spec.id, app.ID, true); err != nil {
				t.Fatalf("stuck %s: %v", spec.id, err)
			}
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	rec := httptest.NewRecorder()
	s.handleOverview(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("overview: status %d body %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Stats     model.OverviewStats    `json:"stats"`
		Recent    []model.OverviewRecent `json:"recent"`
		Attention []model.AttentionItem  `json:"attention"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Stats.Total != 5 || got.Stats.Online != 4 || got.Stats.Offline != 1 || got.Stats.Apps != 4 {
		t.Fatalf("stats base: %+v", got.Stats)
	}
	if got.Stats.Synced != 1 || got.Stats.Drift != 2 || got.Stats.Failed != 1 || got.Stats.Unknown != 0 {
		t.Fatalf("stats app states: %+v", got.Stats)
	}
	if got.Stats.HostOK != 1 || got.Stats.HostDrift != 1 || got.Stats.HostError != 1 || got.Stats.HostUnknown != 1 || got.Stats.HostOffline != 1 {
		t.Fatalf("stats host states: %+v", got.Stats)
	}
	if len(got.Stats.Versions) != 2 || got.Stats.Versions[0].Version != "v1" || got.Stats.Versions[0].Count != 4 {
		t.Fatalf("stats versions: %+v", got.Stats.Versions)
	}
	if len(got.Recent) != 4 || got.Recent[0].Application.ID != app.ID || got.Recent[0].Run.Status == "" {
		t.Fatalf("recent: %+v", got.Recent)
	}
	kinds := map[string]bool{}
	for _, item := range got.Attention {
		kinds[item.Kind] = true
		if item.Kind == model.StateOffline && item.Application != nil {
			t.Fatalf("offline item should not carry an app: %+v", item)
		}
	}
	if len(got.Attention) != 4 || !kinds[model.StateOffline] || !kinds["failed"] || !kinds["stuck"] || !kinds[model.StateDrift] {
		t.Fatalf("attention: %+v", got.Attention)
	}
}
