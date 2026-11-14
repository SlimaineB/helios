package gitops

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// gitTimeout bounds every git subprocess so an unreachable or hung repo server
// cannot stall the agent loop forever.
const gitTimeout = 60 * time.Second

// Repo is a local mirror of a git repository pinned to a branch or tag.
type Repo struct {
	Dir    string
	URL    string
	Target string // branch, tag, SHA or semver range
	// fromTag marks Target as an already-resolved tag; fromSHA marks Target as
	// a commit SHA (rollback pin). cloned marks a mirror that was just created
	// here (checkout refs differ from a fetch).
	fromTag bool
	fromSHA bool
	cloned  bool
}

// Creds is the git connection: optional HTTP credentials (passed to git via
// GIT_ASKPASS) or an SSH private key (materialized to a temp file).
type Creds struct {
	Username           string
	Password           string
	SSHKey             string
	InsecureSkipVerify bool
}

// OpenSync ensures the mirror exists at Dir and is at the target revision
// (branch, tag, SHA, or latest matching semver tag). It returns the commit SHA
// before and after the operation so callers can detect "did anything change?"
// cheaply via before != after.
func OpenSync(ctx context.Context, dir, url, target string, c Creds) (before, after string, err error) {
	r := Repo{Dir: dir, URL: url, Target: target}

	env, cleanup, err := c.env()
	if err != nil {
		return "", "", err
	}
	defer cleanup()

	if isSemverRange(target) {
		tag, err := r.latestTag(ctx, c, env, target)
		if err != nil {
			return "", "", err
		}
		r.Target = tag
		r.fromTag = true
	} else if isCommitSHA(target) {
		r.fromSHA = true
	}

	before, _ = r.head(ctx)
	if _, statErr := os.Stat(filepath.Join(dir, ".git")); statErr != nil {
		if err := r.clone(ctx, c, env); err != nil {
			return before, "", err
		}
		r.cloned = true
	} else {
		// Re-pin origin every run so credential changes take effect.
		if err := r.setOrigin(ctx, c, env); err != nil {
			return before, "", err
		}
		if err := r.fetch(ctx, c, env); err != nil {
			return before, "", err
		}
	}

	after, err = r.checkout(ctx, env)
	return before, after, err
}

// env builds the extra process environment plus a cleanup func.
//
// HTTP credentials go through a short-lived GIT_ASKPASS script instead of being
// embedded in the remote URL: they never land in .git/config, in `ps` argv, or
// in git's error output. With an SSH key, the key is written to a temp file and
// exposed via GIT_SSH_COMMAND (IdentitiesOnly so only that key is offered).
func (c Creds) env() (_ []string, cleanup func(), err error) {
	cleanup = func() {}
	if c.SSHKey != "" {
		f, err := os.CreateTemp("", "helios-repo-*")
		if err != nil {
			return nil, cleanup, fmt.Errorf("temp ssh key: %w", err)
		}
		cleanup = func() { os.Remove(f.Name()) }
		if _, err := f.WriteString(c.SSHKey); err != nil {
			f.Close()
			return nil, cleanup, err
		}
		if err := f.Close(); err != nil {
			return nil, cleanup, err
		}
		if err := os.Chmod(f.Name(), 0o600); err != nil {
			return nil, cleanup, err
		}
		ssh := "ssh -i " + f.Name() + " -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new"
		return []string{"GIT_SSH_COMMAND=" + ssh}, cleanup, nil
	}
	if c.Username == "" && c.Password == "" {
		return nil, cleanup, nil
	}
	user, pass := c.Username, c.Password
	if user == "" {
		user, pass = c.Password, ""
	}
	f, err := os.CreateTemp("", "helios-askpass-*")
	if err != nil {
		return nil, cleanup, fmt.Errorf("temp askpass: %w", err)
	}
	cleanup = func() { os.Remove(f.Name()) }
	script := "#!/bin/sh\ncase \"$1\" in\n  *[Uu]sername*) printf '%s\\n' \"$HELIOS_GIT_USERNAME\" ;;\n  *) printf '%s\\n' \"$HELIOS_GIT_PASSWORD\" ;;\nesac\n"
	if _, err := f.WriteString(script); err != nil {
		f.Close()
		return nil, cleanup, err
	}
	if err := f.Close(); err != nil {
		return nil, cleanup, err
	}
	if err := os.Chmod(f.Name(), 0o700); err != nil {
		return nil, cleanup, err
	}
	return []string{
		"GIT_ASKPASS=" + f.Name(),
		"GIT_TERMINAL_PROMPT=0",
		"HELIOS_GIT_USERNAME=" + user,
		"HELIOS_GIT_PASSWORD=" + pass,
	}, cleanup, nil
}

// gitArgs prefixes global config (-c) before a subcommand.
func gitArgs(insecure bool, args ...string) []string {
	if !insecure {
		return args
	}
	out := make([]string, 0, len(args)+2)
	out = append(out, args[0], "-c", "http.sslVerify=false")
	out = append(out, args[1:]...)
	return out
}

// RevParse returns the commit SHA at HEAD.
func (r *Repo) head(ctx context.Context) (string, error) {
	out, err := runGit(ctx, nil, "git", "-C", r.Dir, "rev-parse", "HEAD")
	return strings.TrimSpace(string(out)), err
}

// Head returns the commit SHA of an existing git work tree. It is empty for a
// snapshot extraction, which has no .git directory.
func Head(ctx context.Context, dir string) (string, error) {
	r := Repo{Dir: dir}
	return r.head(ctx)
}

func (r *Repo) clone(ctx context.Context, c Creds, env []string) error {
	// The directory may hold a snapshot extraction (no .git) after the cache
	// went away. A git clone refuses non-empty targets, so clear it first.
	if _, err := os.Stat(filepath.Join(r.Dir, ".git")); err != nil {
		if err := os.RemoveAll(r.Dir); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return err
	}
	// A SHA is not a branch/tag: clone every branch (so the commit is
	// reachable) and let checkout detach on the requested commit.
	args := []string{"git", "clone", "--no-single-branch"}
	if !r.fromSHA {
		args = append(args, "--branch", r.Target)
	}
	args = append(args, r.URL, r.Dir)
	out, err := runGit(ctx, env, gitArgs(c.InsecureSkipVerify, args...)...)
	if err != nil {
		return fmt.Errorf("git clone %s: %w: %s", r.URL, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (r *Repo) setOrigin(ctx context.Context, c Creds, env []string) error {
	out, err := runGit(ctx, env, "git", "-C", r.Dir, "remote", "set-url", "origin", r.URL)
	if err != nil {
		return fmt.Errorf("git remote set-url: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (r *Repo) fetch(ctx context.Context, c Creds, env []string) error {
	var args []string
	switch {
	case r.fromTag:
		// Fetch the exact tag so refs/tags/<name> and its annotated object are
		// materialized locally; dumb-HTTP servers expose it via info/refs.
		args = gitArgs(c.InsecureSkipVerify,
			"git", "-C", r.Dir, "fetch", "origin", "tag", r.Target)
	case r.fromSHA:
		// Fetch every branch: the pinned commit must be reachable, and servers
		// commonly reject fetching an arbitrary SHA by name.
		args = gitArgs(c.InsecureSkipVerify, "git", "-C", r.Dir, "fetch", "origin")
	default:
		args = gitArgs(c.InsecureSkipVerify, "git", "-C", r.Dir, "fetch", "origin", "--", r.Target)
	}
	out, err := runGit(ctx, env, args...)
	if err != nil {
		return fmt.Errorf("git fetch: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// checkout hard-checks out the fetched/cloned revision (dedicated work tree).
// Git no longer writes FETCH_HEAD on a fresh clone (only after a fetch), so a
// newly-cloned mirror checks out its remote-tracking branch instead.
func (r *Repo) checkout(ctx context.Context, env []string) (string, error) {
	ref := "FETCH_HEAD"
	switch {
	case r.fromSHA:
		ref = r.Target
	case r.fromTag:
		ref = "refs/tags/" + r.Target
	case r.cloned:
		ref = "origin/" + r.Target
	}
	cmds := [][]string{
		{"git", "-C", r.Dir, "checkout", "-f", ref},
		{"git", "-C", r.Dir, "reset", "--hard", ref},
		{"git", "-C", r.Dir, "clean", "-fdx"},
	}
	for _, args := range cmds {
		if out, err := runGit(ctx, env, args...); err != nil {
			return "", fmt.Errorf("%s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return r.head(ctx)
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

// isCommitSHA reports whether s is a full or abbreviated commit SHA. Pinned
// rollbacks arrive through the same envelope as branches, so callers must tell
// them apart: a SHA cannot be passed to `git clone --branch`.
func isCommitSHA(s string) bool {
	if len(s) < 7 || len(s) > 40 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// Subject returns the one-line subject of a commit (powering the commit
// message shown in the UI). It tolerates unknown SHAs by returning whatever
// git prints.
func (r *Repo) Subject(ctx context.Context, sha string) (string, error) {
	if sha == "" {
		return "", nil
	}
	out, err := runGit(ctx, nil, "git", "-C", r.Dir, "log", "-1", "--format=%s", sha)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// PlaybookPath resolves the relative playbook path (file or dir/site.yml)
// inside the repo. It refuses paths that escape the repo.
func (r *Repo) PlaybookPath(rel string) (string, error) {
	p := filepath.Join(r.Dir, rel)
	cwd, err := filepath.Abs(r.Dir)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	relPath, err := filepath.Rel(cwd, abs)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("playbook path escapes repo: %s", rel)
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("playbook not found in repo: %s", rel)
	}
	if fi.IsDir() {
		return filepath.Join(abs, "site.yml"), nil
	}
	return abs, nil
}
