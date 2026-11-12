// Package repocache keeps bare git mirrors and immutable tarball snapshots on
// local disk so agents can fetch a commit once from the hub instead of cloning
// (or fetching) every repository from the upstream git server.
package repocache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/slim/helios/server/internal/gitops"
)

const gitTimeout = 5 * time.Minute

var (
	// ErrUnsupportedRevision lets callers fall back to a regular git sync for
	// revision syntax the cache does not resolve (notably semver ranges).
	ErrUnsupportedRevision = errors.New("revision not supported by repo cache")
)

// Locker serializes mirror updates across goroutines and across replicas.
type Locker interface {
	AcquireLock(ctx context.Context, key string) (func(), error)
}

// Config controls cache storage and eviction.
type Config struct {
	Dir           string
	MaxBytes      int64         // 0 disables the size cap
	TTL           time.Duration // 0 disables age-based eviction
	ResolveTTL    time.Duration // reuse a branch resolution for this long (0 = always fetch)
	MaxConcurrent int           // concurrent cache builds per replica (0 = default 4)
}

// Cache owns the mirror/snapshot tree rooted at Config.Dir.
type Cache struct {
	cfg    Config
	locker Locker
	log    *slog.Logger
	slots  chan struct{}
}

type resolvedCommit struct {
	Commit string    `json:"commit"`
	At     time.Time `json:"at"`
}

// Snapshot describes a resolved commit and, unless NotModified is set, the
// tarball containing that commit's tree.
type Snapshot struct {
	Commit      string
	Subject     string
	Path        string
	NotModified bool
}

func New(cfg Config, locker Locker, log *slog.Logger) (*Cache, error) {
	if cfg.Dir == "" {
		return nil, errors.New("repo cache dir is empty")
	}
	if locker == nil {
		return nil, errors.New("repo cache locker is nil")
	}
	if err := os.MkdirAll(filepath.Join(cfg.Dir, "mirrors"), 0o755); err != nil {
		return nil, fmt.Errorf("create repo cache mirrors: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.Dir, "snapshots"), 0o755); err != nil {
		return nil, fmt.Errorf("create repo cache snapshots: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.Dir, "resolved"), 0o755); err != nil {
		return nil, fmt.Errorf("create repo cache resolutions: %w", err)
	}
	concurrency := cfg.MaxConcurrent
	if concurrency <= 0 {
		concurrency = 4
	}
	return &Cache{cfg: cfg, locker: locker, log: log, slots: make(chan struct{}, concurrency)}, nil
}

// Ensure resolves revision, updates the bare mirror, and materializes the
// commit tarball. If currentCommit already equals the resolved commit, the
// caller's working tree is up to date and NotModified is returned.
func (c *Cache) Ensure(ctx context.Context, repoURL, revision string, creds gitops.Creds, currentCommit string) (Snapshot, error) {
	if strings.HasPrefix(repoURL, "-") {
		return Snapshot{}, fmt.Errorf("invalid repository url")
	}
	revision, err := simpleRevision(revision)
	if err != nil {
		return Snapshot{}, err
	}
	// Bound concurrent git/archive work per replica. Besides CPU and disk,
	// each build holds a dedicated Postgres advisory-lock connection; without
	// this cap a burst of cold repositories can starve the API connection pool.
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
	repoKey := hashKey(repoURL)
	mirror := filepath.Join(c.cfg.Dir, "mirrors", repoKey+".git")

	release, err := c.locker.AcquireLock(ctx, "repo-mirror:"+repoKey)
	if err != nil {
		return Snapshot{}, err
	}
	defer release()

	env, cleanup, err := creds.Env()
	if err != nil {
		return Snapshot{}, err
	}
	defer cleanup()

	if err := ensureMirror(ctx, mirror, repoURL, creds, env); err != nil {
		return Snapshot{}, err
	}
	_ = os.WriteFile(filepath.Join(mirror, ".helios-used"), []byte(time.Now().UTC().Format(time.RFC3339)), 0o644)

	resolveKey := repoKey + "\x00" + revision
	last, haveLast := c.readResolved(resolveKey)
	useCached := haveLast && last.Commit != "" &&
		(isHexSHA(revision) || (c.cfg.ResolveTTL > 0 && time.Since(last.At) < c.cfg.ResolveTTL))
	// A resolution file can outlive the mirror or snapshot it points at (mirror
	// pruned, PVC restored, interrupted fetch). Never trust it unless the commit
	// object is actually present locally.
	if useCached && !hasCommit(ctx, env, mirror, last.Commit) {
		useCached = false
	}

	commit := last.Commit
	if !useCached {
		fetched, ferr := fetchCommit(ctx, mirror, revision, creds, env)
		if ferr != nil {
			// The upstream is down but we may still have a usable stale commit.
			// Only serve it when the object is present: otherwise git archive
			// would fail later with a misleading error.
			if !haveLast || last.Commit == "" || !hasCommit(ctx, env, mirror, last.Commit) {
				return Snapshot{}, ferr
			}
			if c.log != nil {
				c.log.Warn("repo fetch failed; serving last cached commit",
					"repo", repoURL, "revision", revision, "commit", last.Commit[:min(12, len(last.Commit))], "error", ferr)
			}
			commit = last.Commit
		} else {
			commit = fetched
			c.writeResolved(resolveKey, resolvedCommit{Commit: commit, At: time.Now()})
		}
	}

	if currentCommit != "" && currentCommit == commit {
		return Snapshot{Commit: commit, NotModified: true}, nil
	}

	snapshotPath := filepath.Join(c.cfg.Dir, "snapshots", repoKey, commit+".tar.gz")
	if _, err := os.Stat(snapshotPath); err != nil {
		if !os.IsNotExist(err) {
			return Snapshot{}, fmt.Errorf("stat snapshot: %w", err)
		}
		if err := c.writeSnapshot(ctx, mirror, commit, snapshotPath, creds, env); err != nil {
			return Snapshot{}, err
		}
	} else {
		now := time.Now()
		_ = os.Chtimes(snapshotPath, now, now)
	}

	subject, _ := runGit(ctx, env, "git", "-C", mirror, "log", "-1", "--format=%s", commit)
	return Snapshot{
		Commit:  commit,
		Subject: strings.TrimSpace(string(subject)),
		Path:    snapshotPath,
	}, nil
}

func ensureMirror(ctx context.Context, mirror, repoURL string, creds gitops.Creds, env []string) error {
	if _, err := os.Stat(filepath.Join(mirror, "HEAD")); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("stat mirror: %w", err)
		}
		if err := os.RemoveAll(mirror); err != nil {
			return fmt.Errorf("clear mirror: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(mirror), 0o755); err != nil {
			return fmt.Errorf("create mirror parent: %w", err)
		}
		if out, err := runGit(ctx, env, "git", "init", "--bare", mirror); err != nil {
			return fmt.Errorf("git init --bare: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}
	if out, err := runGit(ctx, env, "git", "-C", mirror, "remote", "get-url", "origin"); err != nil || strings.TrimSpace(string(out)) != repoURL {
		if _, err := runGit(ctx, env, "git", "-C", mirror, "remote", "remove", "origin"); err != nil {
			// A fresh bare repo has no origin; removing it is best-effort.
		}
		if out, err := runGit(ctx, env, "git", "-C", mirror, "remote", "add", "origin", repoURL); err != nil {
			return fmt.Errorf("git remote add origin: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}
	if _, err := runGit(ctx, env, "git", "-C", mirror, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		return fmt.Errorf("git config fetch refspec")
	}
	return nil
}

func fetchCommit(ctx context.Context, mirror, revision string, creds gitops.Creds, env []string) (string, error) {
	args := gitArgs(creds.InsecureSkipVerify, "git", "-C", mirror, "fetch", "--no-tags", "--prune", "origin", "--", revision)
	out, err := runGit(ctx, env, args...)
	if err != nil && isHexSHA(revision) {
		// Some servers do not allow fetching arbitrary reachable SHAs. A full
		// fetch gives the object a home before we try again.
		if out2, err2 := runGit(ctx, env, gitArgs(creds.InsecureSkipVerify, "git", "-C", mirror, "fetch", "--prune", "origin")...); err2 != nil {
			return "", fmt.Errorf("git fetch: %w: %s", err, strings.TrimSpace(string(out2)))
		}
		out, err = runGit(ctx, env, args...)
	}
	if err != nil {
		return "", fmt.Errorf("git fetch %s: %w: %s", revision, err, strings.TrimSpace(string(out)))
	}
	sha, err := runGit(ctx, env, "git", "-C", mirror, "rev-parse", "FETCH_HEAD^{commit}")
	if err != nil {
		return "", fmt.Errorf("git rev-parse FETCH_HEAD: %w: %s", err, strings.TrimSpace(string(sha)))
	}
	return strings.TrimSpace(string(sha)), nil
}

func hasCommit(ctx context.Context, env []string, mirror, commit string) bool {
	if commit == "" {
		return false
	}
	_, err := runGit(ctx, env, "git", "-C", mirror, "cat-file", "-e", commit+"^{commit}")
	return err == nil
}

func (c *Cache) writeSnapshot(ctx context.Context, mirror, commit, dst string, creds gitops.Creds, env []string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create snapshot dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".snapshot-*.tmp")
	if err != nil {
		return fmt.Errorf("create snapshot temp: %w", err)
	}
	tmpName := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpName)

	args := gitArgs(creds.InsecureSkipVerify,
		"git", "-C", mirror, "archive", "--format=tar.gz", "-o", tmpName, commit)
	if out, err := runGit(ctx, env, args...); err != nil {
		return fmt.Errorf("git archive: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return fmt.Errorf("publish snapshot: %w", err)
	}
	return nil
}

func (c *Cache) resolvedPath(key string) string {
	return filepath.Join(c.cfg.Dir, "resolved", hashKey(key)+".json")
}

func (c *Cache) readResolved(key string) (resolvedCommit, bool) {
	var r resolvedCommit
	b, err := os.ReadFile(c.resolvedPath(key))
	if err != nil {
		return r, false
	}
	if err := json.Unmarshal(b, &r); err != nil || !isHexSHA(r.Commit) {
		return resolvedCommit{}, false
	}
	return r, true
}

func (c *Cache) writeResolved(key string, r resolvedCommit) {
	b, err := json.Marshal(r)
	if err != nil {
		return
	}
	path := c.resolvedPath(key)
	tmp, err := os.CreateTemp(filepath.Dir(path), ".resolved-*")
	if err != nil {
		return
	}
	name := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(name)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return
	}
	_ = os.Rename(name, path)
}

// Prune removes old snapshots and then the least-recently-used ones until the
// snapshot tree is under the configured size cap. Mirrors are kept: they are
// small compared with repeated tarballs and make the next sync incremental.
func (c *Cache) Prune(ctx context.Context) error {
	c.pruneMirrors(ctx)

	// Resolution files are tiny, but they accumulate for deleted apps/revs.
	// Keep a month; the next request simply fetches again.
	resolvedCutoff := time.Now().Add(-30 * 24 * time.Hour)
	_ = filepath.WalkDir(filepath.Join(c.cfg.Dir, "resolved"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().Before(resolvedCutoff) {
			_ = os.Remove(path)
		}
		return nil
	})

	root := filepath.Join(c.cfg.Dir, "snapshots")
	type entry struct {
		path string
		size int64
		mod  time.Time
	}
	var files []entry
	var total int64
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".tar.gz") && !strings.HasSuffix(d.Name(), ".tmp") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".tmp") {
			if time.Since(info.ModTime()) > time.Hour {
				_ = os.Remove(path)
			}
			return nil
		}
		files = append(files, entry{path: path, size: info.Size(), mod: info.ModTime()})
		total += info.Size()
		return nil
	})
	if err != nil {
		return err
	}

	if c.cfg.TTL > 0 {
		cutoff := time.Now().Add(-c.cfg.TTL)
		kept := files[:0]
		for _, f := range files {
			if f.mod.Before(cutoff) {
				if err := os.Remove(f.path); err == nil {
					total -= f.size
					continue
				}
			}
			kept = append(kept, f)
		}
		files = kept
	}
	if c.cfg.MaxBytes > 0 && total > c.cfg.MaxBytes {
		sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
		for i, f := range files {
			if total <= c.cfg.MaxBytes {
				break
			}
			// Keep the newest snapshot and never evict one touched in the last
			// minute: it is most likely being generated or served right now.
			// A single snapshot larger than the cap is also kept, otherwise the
			// cache would delete and regenerate it forever.
			if i == len(files)-1 || time.Since(f.mod) < time.Minute {
				continue
			}
			if err := os.Remove(f.path); err == nil {
				total -= f.size
			}
		}
		if total > c.cfg.MaxBytes && c.log != nil {
			c.log.Warn("repo cache is above its size cap", "bytes", total, "max_bytes", c.cfg.MaxBytes)
		}
	}
	if c.log != nil && total != 0 {
		c.log.Debug("repo cache pruned", "bytes", total, "files", len(files))
	}
	return nil
}

// pruneMirrors removes mirrors unused for a month. Active repositories are
// kept so the next fetch stays incremental; deleted apps eventually free their
// history. The per-repo advisory lock prevents racing an in-flight fetch.
func (c *Cache) pruneMirrors(ctx context.Context) {
	const retention = 30 * 24 * time.Hour
	root := filepath.Join(c.cfg.Dir, "mirrors")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-retention)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		unusedSince := time.Time{}
		if info, err := e.Info(); err == nil {
			unusedSince = info.ModTime()
		}
		marker := filepath.Join(dir, ".helios-used")
		if info, err := os.Stat(marker); err == nil {
			unusedSince = info.ModTime()
		}
		if !unusedSince.Before(cutoff) {
			continue
		}
		key := strings.TrimSuffix(e.Name(), ".git")
		release, err := c.locker.AcquireLock(ctx, "repo-mirror:"+key)
		if err != nil {
			continue
		}
		// Re-check under the lock: another request may have refreshed it.
		if info, err := os.Stat(marker); err == nil {
			unusedSince = info.ModTime()
		} else if info, err := os.Stat(dir); err == nil {
			unusedSince = info.ModTime()
		}
		if unusedSince.Before(cutoff) {
			if err := os.RemoveAll(dir); err == nil && c.log != nil {
				c.log.Debug("removed unused repo mirror", "repo", e.Name())
			}
		}
		release()
	}
}

// Start runs a periodic prune until ctx is cancelled. It is safe to call only
// once per cache instance.
func (c *Cache) Start(ctx context.Context) {
	if c.cfg.TTL <= 0 && c.cfg.MaxBytes <= 0 {
		return
	}
	go func() {
		_ = c.Prune(ctx)
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = c.Prune(ctx)
			}
		}
	}()
}

func simpleRevision(revision string) (string, error) {
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return "main", nil
	}
	// Semver ranges are resolved by the agent's git implementation. Cache the
	// common single-ref cases (branch, tag, SHA) and fall back for the rest.
	if strings.HasPrefix(revision, "-") ||
		strings.ContainsAny(revision, "*<>=^~, |:\\") ||
		strings.HasSuffix(revision, ".x") ||
		strings.HasSuffix(revision, ".X") {
		return "", ErrUnsupportedRevision
	}
	return revision, nil
}

func isHexSHA(s string) bool {
	if len(s) < 7 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func hashKey(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

func gitArgs(insecure bool, args ...string) []string {
	if !insecure {
		return args
	}
	out := make([]string, 0, len(args)+2)
	out = append(out, args[0], "-c", "http.sslVerify=false")
	out = append(out, args[1:]...)
	return out
}

func runGit(ctx context.Context, env []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = append(os.Environ(), env...)
	// A git helper (ssh, credential helper) can inherit stdout/stderr and keep
	// the pipe open after the main process is killed. WaitDelay prevents
	// CombinedOutput from blocking forever in that case.
	cmd.WaitDelay = 10 * time.Second
	return cmd.CombinedOutput()
}
