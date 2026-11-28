package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/slim/helios/server/internal/model"
)

func TestInstanceFilteringAndBatchLoaders(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "scale.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	now := time.Now().UTC()

	app, err := st.CreateApplication(ctx, model.Application{Name: "web", RepoURL: "https://example.com/web.git", Branch: "main", Path: "site.yml", Vars: "{}"})
	if err != nil {
		t.Fatalf("create app: %v", err)
	}
	repo, err := st.CreateRepository(ctx, model.Repository{Name: "web", URL: app.RepoURL, Username: "u", Password: "p"})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	if err := st.SaveAppVariables(ctx, app.ID, []model.Variable{{Key: "env", Value: "prod"}}); err != nil {
		t.Fatalf("save app vars: %v", err)
	}

	mk := func(id, project, region string, tags []string, mode string, seen time.Time, state string) model.Instance {
		inst, err := st.CreateInstance(ctx, model.Instance{ID: id, Name: id, TokenHash: "tok-" + id, Project: project, Region: region, Tags: tags, ExecutionMode: mode})
		if err != nil {
			t.Fatalf("create instance %s: %v", id, err)
		}
		if err := st.TouchInstance(ctx, id, "v1", id, "linux", 5, seen); err != nil {
			t.Fatalf("touch instance %s: %v", id, err)
		}
		if err := st.UpdateInstanceState(ctx, id, state, ""); err != nil {
			t.Fatalf("state instance %s: %v", id, err)
		}
		return inst
	}
	web1 := mk("web-1", "prod", "eu-west-1", []string{"prod", "team-a"}, model.ExecAgent, now, model.StateDrift)
	web2 := mk("web-2", "", "us-east-1", []string{"prod"}, model.ExecAgent, now.Add(-20*time.Minute), model.StateOK)
	web3 := mk("web-3", "staging", "eu-west-1", []string{"staging"}, model.ExecAgent, now, model.StateUnknown)

	for _, id := range []string{web1.ID, web2.ID, web3.ID} {
		if err := st.Assign(ctx, id, app.ID); err != nil {
			t.Fatalf("assign %s: %v", id, err)
		}
	}
	old, err := st.CreateRun(ctx, model.Run{InstanceID: web1.ID, AppID: app.ID, Mode: "check", Status: model.RunOK, StartedAt: now.Add(-time.Hour), FinishedAt: now.Add(-time.Hour)})
	if err != nil {
		t.Fatalf("create old run: %v", err)
	}
	drift, err := st.CreateRun(ctx, model.Run{InstanceID: web1.ID, AppID: app.ID, Mode: "check", Status: model.RunDrift, StartedAt: now, FinishedAt: now})
	if err != nil {
		t.Fatalf("create drift run: %v", err)
	}
	if _, err := st.CreateRun(ctx, model.Run{InstanceID: web2.ID, AppID: app.ID, Mode: "check", Status: model.RunOK, StartedAt: now, FinishedAt: now}); err != nil {
		t.Fatalf("create ok run: %v", err)
	}

	cases := []struct {
		name string
		f    model.InstanceListFilter
		want []string
	}{
		{"all", model.InstanceListFilter{AllProjects: true}, []string{"web-1", "web-2", "web-3"}},
		{"query", model.InstanceListFilter{AllProjects: true, Query: "web-2"}, []string{"web-2"}},
		{"app search", model.InstanceListFilter{AllProjects: true, Query: "web"}, []string{"web-1", "web-2", "web-3"}},
		{"multi term", model.InstanceListFilter{AllProjects: true, Query: "web staging"}, []string{"web-3"}},
		{"tag", model.InstanceListFilter{AllProjects: true, Tag: "PROD"}, []string{"web-1", "web-2"}},
		{"project", model.InstanceListFilter{AllProjects: true, HasProject: true, Project: "prod"}, []string{"web-1"}},
		{"global project", model.InstanceListFilter{AllProjects: true, HasProject: true}, []string{"web-2"}},
		{"app", model.InstanceListFilter{AllProjects: true, AppID: app.ID}, []string{"web-1", "web-2", "web-3"}},
		{"state drift", model.InstanceListFilter{AllProjects: true, State: model.StateDrift}, []string{"web-1"}},
		{"state ok", model.InstanceListFilter{AllProjects: true, State: model.StateOK}, []string{"web-2"}},
		{"state unknown", model.InstanceListFilter{AllProjects: true, State: model.StateUnknown}, []string{"web-3"}},
		{"online", model.InstanceListFilter{AllProjects: true, OnlineOnly: true}, []string{"web-1", "web-3"}},
		{"offline", model.InstanceListFilter{AllProjects: true, State: model.StateOffline}, []string{"web-2"}},
		{"scoped", model.InstanceListFilter{Projects: []string{"prod"}}, []string{"web-1"}},
		{"scoped empty", model.InstanceListFilter{}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := st.ListInstancesFiltered(ctx, tc.f)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			ids := make([]string, len(got))
			for i, inst := range got {
				ids[i] = inst.ID
			}
			if len(ids) != len(tc.want) {
				t.Fatalf("got %v want %v", ids, tc.want)
			}
			for i := range ids {
				if ids[i] != tc.want[i] {
					t.Fatalf("got %v want %v", ids, tc.want)
				}
			}
			n, err := st.CountInstancesFiltered(ctx, tc.f)
			if err != nil || n != len(tc.want) {
				t.Fatalf("count=%d err=%v want %d", n, err, len(tc.want))
			}
		})
	}

	page, err := st.ListInstancesFiltered(ctx, model.InstanceListFilter{AllProjects: true, Limit: 1, Offset: 1})
	if err != nil || len(page) != 1 || page[0].ID != "web-2" {
		t.Fatalf("pagination: err=%v page=%+v", err, page)
	}

	facets, err := st.InstanceFacets(ctx, true, nil)
	if err != nil {
		t.Fatalf("facets: %v", err)
	}
	if len(facets.Projects) != 3 || len(facets.Tags) != 3 || len(facets.Apps) != 1 || facets.Apps[0].ID != app.ID {
		t.Fatalf("facets: %+v", facets)
	}

	assignments, err := st.ListAssignmentsWithApplications(ctx, []string{web1.ID})
	if err != nil || len(assignments) != 1 || assignments[0].Application.ID != app.ID {
		t.Fatalf("assignments with apps: err=%v %+v", err, assignments)
	}
	runs, err := st.ListLatestRuns(ctx, []string{web1.ID}, []string{"check"})
	if err != nil || len(runs) != 1 || runs[0].ID != drift.ID || runs[0].ID == old.ID {
		t.Fatalf("latest runs: err=%v %+v", err, runs)
	}
	repos, err := st.RepositoriesByProjectURLs(ctx, app.Project, []string{app.RepoURL})
	if err != nil || repos[app.RepoURL].ID != repo.ID || repos[app.RepoURL].Password != "p" {
		t.Fatalf("repos by urls: err=%v %+v", err, repos)
	}
	appVars, err := st.ListAppVariablesForApps(ctx, []int64{app.ID})
	if err != nil || len(appVars[app.ID]) != 1 || appVars[app.ID][0].Key != "env" {
		t.Fatalf("app vars grouped: err=%v %+v", err, appVars)
	}
	if err := st.SaveAssignmentVariables(ctx, web1.ID, app.ID, []model.Variable{{Key: "host", Value: "one"}}); err != nil {
		t.Fatalf("save host vars: %v", err)
	}
	hostVars, err := st.ListAssignmentVariablesForApps(ctx, web1.ID, []int64{app.ID})
	if err != nil || len(hostVars[app.ID]) != 1 || hostVars[app.ID][0].Value != "one" {
		t.Fatalf("host vars grouped: err=%v %+v", err, hostVars)
	}

	overview, err := st.InstanceOverviewStats(ctx, true, nil, now)
	if err != nil {
		t.Fatalf("overview stats: %v", err)
	}
	if overview.Total != 3 || overview.Online != 2 || overview.Offline != 1 || overview.Apps != 3 {
		t.Fatalf("overview base stats: %+v", overview)
	}
	if overview.HostDrift != 1 || overview.HostOffline != 1 || overview.HostUnknown != 1 {
		t.Fatalf("overview host stats: %+v", overview)
	}
	if len(overview.Versions) != 1 || overview.Versions[0].Version != "v1" || overview.Versions[0].Count != 3 {
		t.Fatalf("overview versions: %+v", overview.Versions)
	}

	byIDs, err := st.InstancesByIDs(ctx, []string{web1.ID, web3.ID})
	if err != nil || len(byIDs) != 2 {
		t.Fatalf("instances by ids: err=%v %+v", err, byIDs)
	}
	appsByIDs, err := st.ApplicationsByIDs(ctx, []int64{app.ID})
	if err != nil || len(appsByIDs) != 1 || appsByIDs[0].ID != app.ID {
		t.Fatalf("applications by ids: err=%v %+v", err, appsByIDs)
	}
	recent, err := st.ListRunsScoped(ctx, true, nil, []string{"check"}, 10)
	if err != nil || len(recent) != 3 {
		t.Fatalf("runs scoped: err=%v %+v", err, recent)
	}
	appStats, err := st.ApplicationAssignmentStats(ctx, true, nil)
	if err != nil || len(appStats) != 1 {
		t.Fatalf("application stats: err=%v %+v", err, appStats)
	}
	if appStats[0].Instances != 3 || appStats[0].Synced != 1 || appStats[0].Drift != 1 || appStats[0].Unknown != 1 || appStats[0].Error != 0 {
		t.Fatalf("application stats values: %+v", appStats[0])
	}

	beforeStuck, err := st.ListDriftAssignments(ctx, model.AssignmentFilter{AllProjects: true})
	if err != nil || len(beforeStuck) != 1 || beforeStuck[0].InstanceID != web1.ID {
		t.Fatalf("drift before stuck: err=%v %+v", err, beforeStuck)
	}
	if err := st.SetStuck(ctx, web2.ID, app.ID, true); err != nil {
		t.Fatalf("set stuck: %v", err)
	}
	stuck, err := st.ListInstancesFiltered(ctx, model.InstanceListFilter{AllProjects: true, StuckOnly: true})
	if err != nil || len(stuck) != 1 || stuck[0].ID != web2.ID {
		t.Fatalf("stuck filter: err=%v %+v", err, stuck)
	}
	driftKeys, err := st.ListDriftAssignments(ctx, model.AssignmentFilter{AllProjects: true})
	if err != nil || len(driftKeys) != 2 {
		t.Fatalf("drift assignments: err=%v %+v", err, driftKeys)
	}
	driftTotal, err := st.CountDriftAssignments(ctx, model.AssignmentFilter{AllProjects: true, Query: "web-1"})
	if err != nil || driftTotal != 1 {
		t.Fatalf("drift count: err=%v %d", err, driftTotal)
	}
	driftMulti, err := st.CountDriftAssignments(ctx, model.AssignmentFilter{AllProjects: true, Query: "web-1 eu-west"})
	if err != nil || driftMulti != 1 {
		t.Fatalf("drift multi count: err=%v %d", err, driftMulti)
	}
	driftNone, err := st.CountDriftAssignments(ctx, model.AssignmentFilter{AllProjects: true, Query: "web-2 eu-west"})
	if err != nil || driftNone != 0 {
		t.Fatalf("drift multi none: err=%v %d", err, driftNone)
	}
	driftStats, err := st.DriftAssignmentStats(ctx, model.AssignmentFilter{AllProjects: true})
	if err != nil || driftStats.Total != 2 || driftStats.Instances != 2 || driftStats.Apps != 1 {
		t.Fatalf("drift stats: err=%v %+v", err, driftStats)
	}
	driftFacets, err := st.DriftAssignmentFacets(ctx, true, nil)
	if err != nil || len(driftFacets.Apps) != 1 || driftFacets.Apps[0].ID != app.ID || len(driftFacets.Projects) != 2 {
		t.Fatalf("drift facets: err=%v %+v", err, driftFacets)
	}
}
