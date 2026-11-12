package repocache

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/slim/helios/server/internal/gitops"
)

type memoryLocker struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (l *memoryLocker) AcquireLock(_ context.Context, key string) (func(), error) {
	l.mu.Lock()
	if l.locks == nil {
		l.locks = map[string]*sync.Mutex{}
	}
	mu := l.locks[key]
	if mu == nil {
		mu = &sync.Mutex{}
		l.locks[key] = mu
	}
	l.mu.Unlock()
	mu.Lock()
	return mu.Unlock, nil
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func makeRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "site.yml"), []byte("- hosts: all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "first")
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return dir, strings.TrimSpace(string(out))
}

func readSnapshot(t *testing.T, path, want string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	files := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			b, _ := io.ReadAll(tr)
			files[hdr.Name] = string(b)
		}
	}
	if got := files["site.yml"]; got != want {
		t.Fatalf("site.yml = %q, want %q", got, want)
	}
	return files
}

func TestEnsureBranchAndNotModified(t *testing.T) {
	repo, commit := makeRepo(t)
	cache, err := New(Config{Dir: t.TempDir()}, &memoryLocker{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	snap, err := cache.Ensure(ctx, repo, "main", gitops.Creds{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Commit != commit || snap.NotModified {
		t.Fatalf("snapshot = %+v, want commit %s", snap, commit)
	}
	readSnapshot(t, snap.Path, "- hosts: all\n")

	again, err := cache.Ensure(ctx, repo, "main", gitops.Creds{}, commit)
	if err != nil {
		t.Fatal(err)
	}
	if !again.NotModified || again.Commit != commit {
		t.Fatalf("second snapshot = %+v, want not-modified", again)
	}

	if err := os.WriteFile(filepath.Join(repo, "site.yml"), []byte("- hosts: web\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "commit", "-am", "second")
	updated, err := cache.Ensure(ctx, repo, "main", gitops.Creds{}, commit)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Commit == commit || updated.NotModified {
		t.Fatalf("updated snapshot = %+v, want a new commit", updated)
	}
	readSnapshot(t, updated.Path, "- hosts: web\n")
}

func TestEnsureTagAndRevisionFallback(t *testing.T) {
	repo, commit := makeRepo(t)
	git(t, repo, "tag", "v1.0.0")
	cache, err := New(Config{Dir: t.TempDir()}, &memoryLocker{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	snap, err := cache.Ensure(ctx, repo, "v1.0.0", gitops.Creds{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Commit != commit {
		t.Fatalf("tag commit = %s, want %s", snap.Commit, commit)
	}

	if _, err := cache.Ensure(ctx, repo, ">=1.0.0", gitops.Creds{}, ""); !errors.Is(err, ErrUnsupportedRevision) {
		t.Fatalf("semver error = %v, want ErrUnsupportedRevision", err)
	}
	if _, err := cache.Ensure(ctx, repo, "main:refs/heads/evil", gitops.Creds{}, ""); !errors.Is(err, ErrUnsupportedRevision) {
		t.Fatalf("refspec error = %v, want ErrUnsupportedRevision", err)
	}
}

func TestResolveTTLReusesBranchCommit(t *testing.T) {
	repo, first := makeRepo(t)
	cache, err := New(Config{Dir: t.TempDir(), ResolveTTL: time.Hour}, &memoryLocker{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	snap, err := cache.Ensure(ctx, repo, "main", gitops.Creds{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Commit != first {
		t.Fatalf("first commit = %s, want %s", snap.Commit, first)
	}

	if err := os.WriteFile(filepath.Join(repo, "site.yml"), []byte("- hosts: web\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "commit", "-am", "second")

	cached, err := cache.Ensure(ctx, repo, "main", gitops.Creds{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if cached.Commit != first {
		t.Fatalf("cached commit = %s, want %s", cached.Commit, first)
	}

	cache.cfg.ResolveTTL = 0
	fresh, err := cache.Ensure(ctx, repo, "main", gitops.Creds{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Commit == first {
		t.Fatalf("resolve TTL disabled but commit stayed %s", first)
	}
}

func TestResolveTTLSharedAcrossCacheInstances(t *testing.T) {
	repo, first := makeRepo(t)
	dir := t.TempDir()
	locker := &memoryLocker{}
	cacheA, err := New(Config{Dir: dir, ResolveTTL: time.Hour}, locker, nil)
	if err != nil {
		t.Fatal(err)
	}
	cacheB, err := New(Config{Dir: dir, ResolveTTL: time.Hour}, locker, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := cacheA.Ensure(ctx, repo, "main", gitops.Creds{}, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "site.yml"), []byte("- hosts: web\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "commit", "-am", "second")

	cached, err := cacheB.Ensure(ctx, repo, "main", gitops.Creds{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if cached.Commit != first {
		t.Fatalf("shared resolution = %s, want %s", cached.Commit, first)
	}
}

func TestPruneRemovesUnusedMirror(t *testing.T) {
	repo, _ := makeRepo(t)
	cache, err := New(Config{Dir: t.TempDir()}, &memoryLocker{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Ensure(context.Background(), repo, "main", gitops.Creds{}, ""); err != nil {
		t.Fatal(err)
	}
	mirror := filepath.Join(cache.cfg.Dir, "mirrors", hashKey(repo)+".git")
	marker := filepath.Join(mirror, ".helios-used")
	old := time.Now().Add(-31 * 24 * time.Hour)
	if err := os.Chtimes(marker, old, old); err != nil {
		t.Fatal(err)
	}
	if err := cache.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mirror); !os.IsNotExist(err) {
		t.Fatalf("unused mirror still exists: %v", err)
	}
}

func TestPruneRemovesSnapshotsOverCap(t *testing.T) {
	repo, _ := makeRepo(t)
	cache, err := New(Config{Dir: t.TempDir()}, &memoryLocker{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := cache.Ensure(ctx, repo, "main", gitops.Creds{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "site.yml"), []byte("- hosts: web\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "commit", "-am", "second")
	second, err := cache.Ensure(ctx, repo, "main", gitops.Creds{}, first.Commit)
	if err != nil {
		t.Fatal(err)
	}
	cache.cfg.MaxBytes = 1
	if err := os.Chtimes(first.Path, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := cache.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first.Path); !os.IsNotExist(err) {
		t.Fatalf("old snapshot still exists: %v", err)
	}
	if _, err := os.Stat(second.Path); err != nil {
		t.Fatalf("newest snapshot was evicted: %v", err)
	}
}

func TestEnsureRecoversAfterMirrorAndSnapshotPruned(t *testing.T) {
	repo, _ := makeRepo(t)
	cache, err := New(Config{Dir: t.TempDir(), ResolveTTL: time.Hour}, &memoryLocker{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	snap, err := cache.Ensure(ctx, repo, "main", gitops.Creds{}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirror := filepath.Join(cache.cfg.Dir, "mirrors", hashKey(repo)+".git")
	if err := os.RemoveAll(mirror); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(snap.Path); err != nil {
		t.Fatal(err)
	}
	recovered, err := cache.Ensure(ctx, repo, "main", gitops.Creds{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Commit != snap.Commit || recovered.NotModified {
		t.Fatalf("recovered snapshot = %+v, want commit %s", recovered, snap.Commit)
	}
	readSnapshot(t, recovered.Path, "- hosts: all\n")
}
