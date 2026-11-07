package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/slim/helios/server/internal/model"
)

func TestLeasesAreExclusiveAndExpire(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "helios.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()
	exerciseLeases(t, st, "gitops:reconcile")
}

func TestPostgresLeasesAreExclusiveAndExpire(t *testing.T) {
	dsn := os.Getenv("HELIOS_TEST_PG")
	if dsn == "" {
		t.Skip("HELIOS_TEST_PG not set")
	}
	st, err := Open(dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer st.Close()
	exerciseLeases(t, st, "gitops:reconcile-test-"+time.Now().UTC().Format("150405.000000000"))
}

func exerciseLeases(t *testing.T, st Store, name string) {
	t.Helper()
	ctx := context.Background()
	defer func() { _ = st.ReleaseLease(ctx, name, "replica-c") }()

	ok, err := st.AcquireLease(ctx, name, "replica-a", time.Minute)
	if err != nil || !ok {
		t.Fatalf("acquire a: ok=%v err=%v", ok, err)
	}
	ok, err = st.AcquireLease(ctx, name, "replica-b", time.Minute)
	if err != nil || ok {
		t.Fatalf("lease should be held by a: ok=%v err=%v", ok, err)
	}
	if err := st.RenewLease(ctx, name, "replica-b", time.Minute); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("renew by non-owner: %v", err)
	}
	if err := st.ReleaseLease(ctx, name, "replica-a"); err != nil {
		t.Fatalf("release a: %v", err)
	}
	ok, err = st.AcquireLease(ctx, name, "replica-b", 10*time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("acquire b after release: ok=%v err=%v", ok, err)
	}
	time.Sleep(20 * time.Millisecond)
	ok, err = st.AcquireLease(ctx, name, "replica-c", time.Minute)
	if err != nil || !ok {
		t.Fatalf("expired lease not reclaimable: ok=%v err=%v", ok, err)
	}
}

func TestPostgresConcurrentOpen(t *testing.T) {
	dsn := os.Getenv("HELIOS_TEST_PG")
	if dsn == "" {
		t.Skip("HELIOS_TEST_PG not set")
	}
	const replicas = 4
	var wg sync.WaitGroup
	errs := make(chan error, replicas)
	stores := make(chan Store, replicas)
	for i := 0; i < replicas; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			st, err := Open(dsn)
			if err != nil {
				errs <- fmt.Errorf("replica %d: %w", n, err)
				return
			}
			stores <- st
		}(i)
	}
	wg.Wait()
	close(errs)
	close(stores)
	for err := range errs {
		t.Error(err)
	}
	for st := range stores {
		_ = st.Close()
	}
}

func TestConcurrentCommandClaim(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "helios.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()
	exerciseConcurrentCommandClaim(t, st, time.Now().UTC().Format("150405.000000000"))
}

func TestPostgresConcurrentCommandClaim(t *testing.T) {
	dsn := os.Getenv("HELIOS_TEST_PG")
	if dsn == "" {
		t.Skip("HELIOS_TEST_PG not set")
	}
	st, err := Open(dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer st.Close()
	exerciseConcurrentCommandClaim(t, st, time.Now().UTC().Format("150405.000000000"))
}

func exerciseConcurrentCommandClaim(t *testing.T, st Store, suffix string) {
	t.Helper()
	ctx := context.Background()
	app, err := st.CreateApplication(ctx, model.Application{
		Name: "claim-" + suffix, RepoURL: "https://example.com/claim.git",
		Branch: "main", Path: "site.yml", Vars: "{}",
	})
	if err != nil {
		t.Fatalf("create app: %v", err)
	}
	defer func() { _ = st.DeleteApplication(ctx, app.ID) }()
	inst, err := st.CreateInstance(ctx, model.Instance{
		ID: "claim-" + suffix, Name: "claim-" + suffix, TokenHash: "claim-" + suffix,
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	defer func() { _ = st.DeleteInstance(ctx, inst.ID) }()
	if err := st.Assign(ctx, inst.ID, app.ID); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if _, err := st.CreateCommand(ctx, model.Command{InstanceID: inst.ID, AppID: app.ID, Action: "check"}); err != nil {
		t.Fatalf("create command: %v", err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	counts := make(chan int, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			cmds, err := st.ClaimCommands(ctx, inst.ID)
			if err != nil {
				errs <- err
				return
			}
			counts <- len(cmds)
		}()
	}
	close(start)
	wg.Wait()
	close(counts)
	close(errs)
	for err := range errs {
		t.Errorf("claim: %v", err)
	}
	total := 0
	for n := range counts {
		total += n
	}
	if total != 1 {
		t.Fatalf("claimed %d commands concurrently, want 1", total)
	}
}
