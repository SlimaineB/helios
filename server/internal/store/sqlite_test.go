package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/slim/helios/server/internal/model"
)

func TestMigrateOldDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{
		`CREATE TABLE applications (
			id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE,
			repo_url TEXT NOT NULL, branch TEXT NOT NULL DEFAULT 'main',
			path TEXT NOT NULL DEFAULT 'site.yml', vars TEXT NOT NULL DEFAULT '{}',
			autoheal INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL)`,
		`CREATE TABLE instances (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, token_hash TEXT NOT NULL UNIQUE,
			version TEXT NOT NULL DEFAULT '', state TEXT NOT NULL DEFAULT 'unknown',
			last_commit TEXT NOT NULL DEFAULT '', last_seen TEXT NOT NULL DEFAULT '',
			hostname TEXT NOT NULL DEFAULT '', platform TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL)`,
		`CREATE TABLE assignments (
			instance_id TEXT NOT NULL, app_id INTEGER NOT NULL, autoheal INTEGER,
			last_check_at TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
			PRIMARY KEY (instance_id, app_id))`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO applications (name, repo_url, created_at) VALUES ('legacy','http://repo/x.git','2024-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO instances (id,name,token_hash,created_at) VALUES ('ins-old','old','hash','2024-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO assignments (instance_id,app_id,created_at) VALUES ('ins-old',1,'2024-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open migrated db: %v", err)
	}
	defer st.Close()

	app, err := st.GetApplication(context.Background(), 1)
	if err != nil {
		t.Fatalf("get legacy app: %v", err)
	}
	if app.Project != "" || app.IgnorePaths != "" || app.SyncInterval != 0 {
		t.Fatalf("new app columns not defaulted: %+v", app)
	}
	a, err := st.GetAssignment(context.Background(), "ins-old", 1)
	if err != nil {
		t.Fatalf("get legacy assignment: %v", err)
	}
	if a.TargetRevision != "" {
		t.Fatalf("target_revision = %q, want empty", a.TargetRevision)
	}
	if _, err := st.CreateProject(context.Background(), model.Project{Name: "finance"}); err != nil {
		t.Fatalf("create project on migrated db: %v", err)
	}
	if _, err := st.CreateSyncWindow(context.Background(), model.SyncWindow{Name: "w", Kind: "deny", Timezone: "UTC", Enabled: true}); err != nil {
		t.Fatalf("create window on migrated db: %v", err)
	}
}
