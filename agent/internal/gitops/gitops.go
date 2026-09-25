package gitops

import (
	"context"
	"fmt"
	"net/url"
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
	// fromTag marks Target as an already-resolved tag; cloned marks a mirror
	// that was just created here (checkout refs differ from a fetch).
	fromTag bool
	cloned  bool
}

// Creds is the git connection: optional HTTP credentials (embedded in the
// remote URL as userinfo) or an SSH private key (materialized to a temp file).
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

// env builds the extra process environment plus a cleanup func. With an SSH
// key it writes the key to a temp file and returns a GIT_SSH_COMMAND that uses
// it (IdentitiesOnly so only that key is offered).
func (c Creds) env() (_ []string, cleanup func(), err error) {
	cleanup = func() {}
	if c.SSHKey == "" {
		return nil, cleanup, nil
	}
	f, err := os.CreateTemp("", "ansiblehub-repo-*")
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

// remoteURL returns the remote with HTTP credentials embedded as userinfo (so
// `git clone`/`fetch` authenticate via basic auth). When only a password is
// set it is used as the token-style user, which GitHub/GitLab accept.
func remoteURL(remote string, c Creds) string {
	if c.Username == "" && c.Password == "" {
		return remote
	}
	u, err := url.Parse(remote)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return remote
	}
	user, pass := c.Username, c.Password
	if user == "" {
		user, pass = c.Password, ""
	}
	if pass == "" {
		u.User = url.User(user)
	} else {
		u.User = url.UserPassword(user, pass)
	}
	return u.String()
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

func (r *Repo) clone(ctx context.Context, c Creds, env []string) error {
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return err
	}
	args := gitArgs(c.InsecureSkipVerify,
		"git", "clone", "--no-single-branch", "--branch", r.Target, remoteURL(r.URL, c), r.Dir)
	out, err := runGit(ctx, env, args...)
	if err != nil {
		return fmt.Errorf("git clone %s: %w: %s", r.URL, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (r *Repo) setOrigin(ctx context.Context, c Creds, env []string) error {
	out, err := runGit(ctx, env, "git", "-C", r.Dir, "remote", "set-url", "origin", remoteURL(r.URL, c))
	if err != nil {
		return fmt.Errorf("git remote set-url: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (r *Repo) fetch(ctx context.Context, c Creds, env []string) error {
	var args []string
	if r.fromTag {
		// Fetch the exact tag so refs/tags/<name> and its annotated object are
		// materialized locally; dumb-HTTP servers expose it via info/refs.
		args = gitArgs(c.InsecureSkipVerify,
			"git", "-C", r.Dir, "fetch", "origin", "tag", r.Target)
	} else {
		args = gitArgs(c.InsecureSkipVerify, "git", "-C", r.Dir, "fetch", "origin", r.Target)
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
	return cmd.CombinedOutput()
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
