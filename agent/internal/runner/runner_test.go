package runner

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestRunner(t *testing.T, cfg Config) *Runner {
	t.Helper()
	if cfg.StateDir == "" {
		cfg.StateDir = t.TempDir()
	}
	return New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestResolveRunnerIDPersists(t *testing.T) {
	dir := t.TempDir()
	r := newTestRunner(t, Config{StateDir: dir})

	first, err := r.resolveRunnerID()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first, "runner-") {
		t.Fatalf("id = %q", first)
	}
	if _, err := os.Stat(filepath.Join(dir, "runner-id")); err != nil {
		t.Fatal(err)
	}

	secondRunner := newTestRunner(t, Config{StateDir: dir})
	second, err := secondRunner.resolveRunnerID()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("id changed: %q != %q", first, second)
	}
}

func TestResolveRunnerIDExplicit(t *testing.T) {
	r := newTestRunner(t, Config{StateDir: t.TempDir(), RunnerID: "runner-edge-1"})
	id, err := r.resolveRunnerID()
	if err != nil {
		t.Fatal(err)
	}
	if id != "runner-edge-1" {
		t.Fatalf("id = %q", id)
	}
}
