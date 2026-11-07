package auth

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/slim/helios/server/internal/store"
)

func TestDBSessionStoreWorksAcrossReplicas(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "helios.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()
	exerciseDBSessionStore(t, st)
}

func TestPostgresDBSessionStoreWorksAcrossReplicas(t *testing.T) {
	dsn := os.Getenv("HELIOS_TEST_PG")
	if dsn == "" {
		t.Skip("HELIOS_TEST_PG not set")
	}
	st, err := store.Open(dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer st.Close()
	exerciseDBSessionStore(t, st)
}

func exerciseDBSessionStore(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()

	replicaA := NewDBSessionStore(st, time.Hour)
	replicaB := NewDBSessionStore(st, time.Hour)
	tok, err := replicaA.Create(ctx, "sub-1", "a@example.com", "A", []string{"sre"}, nil)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sess, ok := replicaB.Get(ctx, tok)
	if !ok || sess.Email != "a@example.com" || len(sess.Groups) != 1 || sess.Groups[0] != "sre" {
		t.Fatalf("session not visible on other replica: %+v ok=%v", sess, ok)
	}
	replicaB.Delete(ctx, tok)
	if _, ok := replicaA.Get(ctx, tok); ok {
		t.Fatal("logout was not applied cluster-wide")
	}

	expired := NewDBSessionStore(st, time.Millisecond)
	tok, err = expired.Create(ctx, "sub-2", "", "", nil, nil)
	if err != nil {
		t.Fatalf("create expiring session: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, ok := replicaA.Get(ctx, tok); ok {
		t.Fatal("expired session accepted")
	}
}
