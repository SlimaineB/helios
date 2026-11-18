package store

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/slim/helios/server/internal/model"
)

func TestPostgresScaleQueries(t *testing.T) {
	dsn := os.Getenv("HELIOS_TEST_PG")
	if dsn == "" {
		t.Skip("HELIOS_TEST_PG not set")
	}
	st, err := Open(dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	suffix := fmt.Sprintf("scale-%d", now.UnixNano())
	project := "p-" + suffix

	mkApp := func(name string) model.Application {
		app, err := st.CreateApplication(ctx, model.Application{Name: name, RepoURL: "https://example.com/" + name + ".git", Branch: "main", Path: "site.yml", Vars: "{}", Project: project})
		if err != nil {
			t.Fatalf("create app %s: %v", name, err)
		}
		return app
	}
	app1 := mkApp(suffix)
	app2 := mkApp(suffix + "-2")

	mkInst := func(id string, tags []string) {
		if _, err := st.CreateInstance(ctx, model.Instance{ID: id, Name: id, TokenHash: "tok-" + id, Project: project, Region: "eu-west-1", Tags: tags}); err != nil {
			t.Fatalf("create instance %s: %v", id, err)
		}
		if err := st.TouchInstance(ctx, id, "v1", id, "linux", 5, now, false); err != nil {
			t.Fatalf("touch instance %s: %v", id, err)
		}
	}
	i1 := suffix + "-1"
	i2 := suffix + "-2"
	i3 := suffix + "-3"
	mkInst(i1, []string{"prod", "team-a"})
	mkInst(i2, []string{"prod"})
	mkInst(i3, nil)
	for _, id := range []string{i1, i2} {
		if err := st.Assign(ctx, id, app1.ID); err != nil {
			t.Fatalf("assign %s app1: %v", id, err)
		}
	}
	if err := st.Assign(ctx, i2, app2.ID); err != nil {
		t.Fatalf("assign i2 app2: %v", err)
	}
	if _, err := st.CreateRun(ctx, model.Run{InstanceID: i1, AppID: app1.ID, Mode: "check", Status: model.RunDrift, StartedAt: now, FinishedAt: now}); err != nil {
		t.Fatalf("create drift run: %v", err)
	}
	if _, err := st.CreateRun(ctx, model.Run{InstanceID: i2, AppID: app1.ID, Mode: "check", Status: model.RunOK, StartedAt: now, FinishedAt: now}); err != nil {
		t.Fatalf("create ok run: %v", err)
	}
	if _, err := st.CreateRun(ctx, model.Run{InstanceID: i2, AppID: app2.ID, Mode: "check", Status: model.RunOK, StartedAt: now, FinishedAt: now}); err != nil {
		t.Fatalf("create app2 ok run: %v", err)
	}
	if err := st.SetStuck(ctx, i2, app2.ID, true); err != nil {
		t.Fatalf("set stuck: %v", err)
	}

	f := model.InstanceListFilter{Projects: []string{project}, NotAppID: app1.ID, Limit: 20}
	got, err := st.ListInstancesFiltered(ctx, f)
	if err != nil {
		t.Fatalf("filter not_app_id: %v", err)
	}
	if len(got) != 1 || got[0].ID != i3 {
		t.Fatalf("not_app_id = %+v, want only %s", got, i3)
	}
	n, err := st.CountInstancesFiltered(ctx, f)
	if err != nil || n != 1 {
		t.Fatalf("count not_app_id = %d err=%v, want 1", n, err)
	}
	facets, err := st.InstanceFacets(ctx, false, []string{project})
	if err != nil {
		t.Fatalf("instance facets: %v", err)
	}
	if len(facets.Apps) != 2 || len(facets.Tags) != 2 {
		t.Fatalf("instance facets = %+v", facets)
	}
	stats, err := st.InstanceListStats(ctx, model.InstanceListFilter{Projects: []string{project}})
	if err != nil || stats.Total != 3 {
		t.Fatalf("instance stats = %+v err=%v", stats, err)
	}
	appStats, err := st.ApplicationAssignmentStats(ctx, false, []string{project})
	if err != nil {
		t.Fatalf("app stats: %v", err)
	}
	byID := map[int64]model.ApplicationStats{}
	for _, s := range appStats {
		byID[s.ID] = s
	}
	if s := byID[app1.ID]; s.Instances != 2 || s.Drift != 1 || s.Synced != 1 {
		t.Fatalf("app1 stats = %+v", s)
	}
	if s := byID[app2.ID]; s.Instances != 1 || s.Stuck != 1 || s.Synced != 1 {
		t.Fatalf("app2 stats = %+v", s)
	}
	af := model.AssignmentFilter{Projects: []string{project}}
	drift, err := st.ListDriftAssignments(ctx, af)
	if err != nil || len(drift) != 2 {
		t.Fatalf("drift assignments = %+v err=%v", drift, err)
	}
	driftTotal, err := st.CountDriftAssignments(ctx, af)
	if err != nil || driftTotal != 2 {
		t.Fatalf("drift count = %d err=%v", driftTotal, err)
	}
	driftStats, err := st.DriftAssignmentStats(ctx, af)
	if err != nil || driftStats.Total != 2 || driftStats.Instances != 2 || driftStats.Apps != 2 {
		t.Fatalf("drift stats = %+v err=%v", driftStats, err)
	}
	driftFacets, err := st.DriftAssignmentFacets(ctx, false, []string{project})
	if err != nil || len(driftFacets.Apps) != 2 {
		t.Fatalf("drift facets = %+v err=%v", driftFacets, err)
	}
}
