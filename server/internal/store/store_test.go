package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/slim/ansiblehub/server/internal/model"
)

func TestSqliteStoreCRUD(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()
	exerciseStore(t, st)
}

func TestRepositoryUpsert(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()
	exerciseUpsert(t, st)

	if dsn := os.Getenv("ANSIBLEHUB_TEST_PG"); dsn != "" {
		pg, err := Open(dsn)
		if err != nil {
			t.Fatalf("open postgres: %v", err)
		}
		defer pg.Close()
		exerciseUpsert(t, pg)
	}
}

func exerciseUpsert(t *testing.T, st Store) {
	ctx := context.Background()

	r1, err := st.EnsureRepositoryByURL(ctx, "http://example.com/a.git")
	if err != nil || r1.ID == 0 || r1.Name != "http://example.com/a.git" {
		t.Fatalf("ensure create: err=%v %+v", err, r1)
	}
	r2, err := st.EnsureRepositoryByURL(ctx, "http://example.com/a.git")
	if err != nil || r2.ID != r1.ID {
		t.Fatalf("ensure idempotent: err=%v r2=%d want=%d", err, r2.ID, r1.ID)
	}

	// An app whose repo_url predates the registry is picked up by backfill.
	app, err := st.CreateApplication(ctx, model.Application{
		Name: "legacy", RepoURL: "http://example.com/b.git", Branch: "main",
		Path: "site.yml", Vars: "{}",
	})
	if err != nil || app.ID == 0 {
		t.Fatalf("create legacy app: %v", err)
	}
	if err := st.BackfillRepositoryRegistry(ctx); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	repos, err := st.ListRepositories(ctx)
	if err != nil {
		t.Fatalf("list repos: %v", err)
	}
	byURL := map[string]int64{}
	for _, r := range repos {
		byURL[r.URL] = r.ID
	}
	if byURL["http://example.com/a.git"] == 0 || byURL["http://example.com/b.git"] == 0 {
		t.Fatalf("registry not complete after backfill: %+v", byURL)
	}
}

// TestPostgresStoreCRUD runs the same battery against a live Postgres when
// ANSIBLEHUB_TEST_PG is set (e.g. postgres://u:p@localhost:5432/ansiblehub_test).
func TestPostgresStoreCRUD(t *testing.T) {
	dsn := os.Getenv("ANSIBLEHUB_TEST_PG")
	if dsn == "" {
		t.Skip("ANSIBLEHUB_TEST_PG not set")
	}
	st, err := Open(dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer st.Close()
	exerciseStore(t, st)
}

func exerciseStore(t *testing.T, st Store) {
	ctx := context.Background()
	now := time.Now()
	name := "smoke-" + now.UTC().Format("150405")

	app, err := st.CreateApplication(ctx, model.Application{
		Name: name, RepoURL: "https://example.com/" + name + ".git",
		Branch: "main", Path: "site.yml", Vars: "{}", AutoHeal: true,
	})
	if err != nil || app.ID == 0 {
		t.Fatalf("create app: %v (id=%d)", err, app.ID)
	}

	got, err := st.GetApplicationByName(ctx, name)
	if err != nil || !got.AutoHeal || got.ID != app.ID {
		t.Fatalf("get app by name: %v got=%+v", err, got)
	}
	got.AutoHeal = false
	if err := st.UpdateApplication(ctx, got); err != nil {
		t.Fatalf("update app: %v", err)
	}
	if a, _ := st.GetApplication(ctx, app.ID); a.AutoHeal {
		t.Fatal("expected autoheal=false after update")
	}

	repo, err := st.CreateRepository(ctx, model.Repository{
		Name: name, URL: "https://example.com/" + name + ".git",
		Username: "u", Password: "p",
	})
	if err != nil || repo.ID == 0 {
		t.Fatalf("create repo: %v (id=%d)", err, repo.ID)
	}
	if r, err := st.GetRepositoryByURL(ctx, repo.URL); err != nil || r.Password != "p" {
		t.Fatalf("get repo by url: %v r=%+v", err, r)
	}

	inst, err := st.CreateInstance(ctx, model.Instance{ID: name, Name: name, TokenHash: "h" + name})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	if i, err := st.GetInstanceByToken(ctx, inst.TokenHash); err != nil || i.ID != inst.ID {
		t.Fatalf("get instance by token: %v i=%+v", err, i)
	}
	inst.State = model.StateDrift
	if err := st.UpdateInstance(ctx, inst); err != nil {
		t.Fatalf("update instance: %v", err)
	}
	if i, _ := st.GetInstance(ctx, inst.ID); i.State != model.StateDrift {
		t.Fatal("expected state=drift after update")
	}

	if err := st.Assign(ctx, inst.ID, app.ID); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := st.Assign(ctx, inst.ID, app.ID); err != ErrAlreadyBound {
		t.Fatalf("expected ErrAlreadyBound, got %v", err)
	}
	if err := st.SetAssignmentAutoHeal(ctx, inst.ID, app.ID, true); err != nil {
		t.Fatalf("set autoheal: %v", err)
	}
	as, err := st.ListAssignments(ctx, inst.ID)
	if err != nil || len(as) != 1 || as[0].AutoHeal == nil || !*as[0].AutoHeal {
		t.Fatalf("list assignments: %v %+v", err, as)
	}

	// Healing guard: streak bumps, stuck flag, reset.
	for i, want := range []int{1, 2, 3} {
		n, err := st.BumpDriftStreak(ctx, inst.ID, app.ID)
		if err != nil || n != want {
			t.Fatalf("bump drift streak #%d: err=%v n=%d want=%d", i+1, err, n, want)
		}
	}
	if err := st.SetStuck(ctx, inst.ID, app.ID, true); err != nil {
		t.Fatalf("set stuck: %v", err)
	}
	a, err := st.GetAssignment(ctx, inst.ID, app.ID)
	if err != nil || !a.Stuck || a.ConsecutiveDrift != 3 || a.StuckAt.IsZero() {
		t.Fatalf("stuck assignment: err=%v %+v", err, a)
	}
	if err := st.ResetHealState(ctx, inst.ID, app.ID); err != nil {
		t.Fatalf("reset heal state: %v", err)
	}
	if a, _ := st.GetAssignment(ctx, inst.ID, app.ID); a.Stuck || a.ConsecutiveDrift != 0 || !a.StuckAt.IsZero() {
		t.Fatalf("expected stuck cleared, got %+v", a)
	}

	vars := []model.Variable{
		{Key: "env", Value: "prod", IsSecret: false},
		{Key: "app_secret", Value: "hunter2", IsSecret: true},
	}
	hvars := []model.Variable{
		{Key: "env", Value: "prod-east", IsSecret: false},
		{Key: "host_secret", Value: "s3cret", IsSecret: true},
	}
	if err := st.SaveAppVariables(ctx, app.ID, vars); err != nil {
		t.Fatalf("save app variables: %v", err)
	}
	if err := st.SaveAssignmentVariables(ctx, inst.ID, app.ID, hvars); err != nil {
		t.Fatalf("save assignment variables: %v", err)
	}
	av, err := st.AppVariables(ctx, app.ID)
	if err != nil || len(av) != 2 || av[0].IsSecret != true {
		t.Fatalf("app variables: %v %+v", err, av)
	}
	ass, err := st.AssignmentVariables(ctx, inst.ID, app.ID)
	if err != nil || len(ass) != 2 || ass[1].Value != "s3cret" {
		t.Fatalf("assignment variables: %v %+v", err, ass)
	}
	if err := st.SaveAppVariables(ctx, app.ID, []model.Variable{{Key: "env", Value: "staging"}}); err != nil {
		t.Fatalf("replace app variables: %v", err)
	}
	if av, err = st.AppVariables(ctx, app.ID); err != nil || len(av) != 1 || av[0].IsSecret {
		t.Fatalf("app variables after replace: %v %+v", err, av)
	}

	run, err := st.CreateRun(ctx, model.Run{
		InstanceID: inst.ID, AppID: app.ID, Mode: "check", Status: model.StateDrift,
		Trigger: "poll", Summary: map[string]int{"changed": 2},
		ChangedTasks: []model.ChangedTask{{Task: "t", Host: "h"}},
		StartedAt:    now, FinishedAt: now,
	})
	if err != nil || run.ID == 0 {
		t.Fatalf("create run: %v (id=%d)", err, run.ID)
	}
	if r, err := st.LatestRun(ctx, inst.ID, app.ID, "check"); err != nil || r.ID != run.ID || r.Summary["changed"] != 2 {
		t.Fatalf("latest run: %v r=%+v", err, r)
	}
	if rs, err := st.ListRuns(ctx, model.RunFilter{InstanceID: inst.ID}); err != nil || len(rs) != 1 {
		t.Fatalf("list runs: %v n=%d", err, len(rs))
	}

	cmd, err := st.CreateCommand(ctx, model.Command{InstanceID: inst.ID, AppID: app.ID, Action: "apply"})
	if err != nil || cmd.ID == 0 {
		t.Fatalf("create command: %v (id=%d)", err, cmd.ID)
	}
	claimed, err := st.ClaimCommands(ctx, inst.ID)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim commands: %v n=%d", err, len(claimed))
	}
	if err := st.CompleteCommand(ctx, cmd.ID, "ok", "all good", "abc123"); err != nil {
		t.Fatalf("complete command: %v", err)
	}
	if c, err := st.LatestCommand(ctx, inst.ID, app.ID, "apply"); err != nil || c.Status != "ok" {
		t.Fatalf("latest command: %v c=%+v", err, c)
	}

	if err := st.Unassign(ctx, inst.ID, app.ID); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	if err := st.DeleteRepository(ctx, repo.ID); err != nil {
		t.Fatalf("delete repo: %v", err)
	}
	if err := st.DeleteApplication(ctx, app.ID); err != nil {
		t.Fatalf("delete app: %v", err)
	}
	if err := st.DeleteInstance(ctx, inst.ID); err != nil {
		t.Fatalf("delete instance: %v", err)
	}
}
