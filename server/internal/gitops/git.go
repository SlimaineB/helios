// Package gitops provides the optional server-side GitOps primitives: a small
// git mirror sync and parsing of Helios YAML manifests. It is deliberately
// independent from the agent's gitops package (separate Go modules).
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

const gitTimeout = 60 * time.Second

// Creds is the git connection used for a config repository. HTTP credentials
// travel through a short-lived GIT_ASKPASS helper; SSH keys are materialized in
// a private temp file. Neither is ever written to the mirror's git config.
type Creds struct {
	Username           string
	Password           string
	SSHKey             string
	InsecureSkipVerify bool
}

// Sync ensures dir is a mirror of url at branch and returns the commit SHA
// before and after the sync.
func Sync(ctx context.Context, dir, url, branch string, c Creds) (before, after string, err error) {
	if branch == "" {
		branch = "main"
	}
	env, cleanup, err := c.env()
	if err != nil {
		return "", "", err
	}
	defer cleanup()

	before = head(ctx, dir)
	cloned := false
	if _, statErr := os.Stat(filepath.Join(dir, ".git")); statErr != nil {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return before, "", err
		}
		args := gitArgs(c.InsecureSkipVerify,
			"git", "clone", "--no-single-branch", "--branch", branch, url, dir)
		if out, err := runGit(ctx, env, args...); err != nil {
			return before, "", fmt.Errorf("git clone %s: %w: %s", url, err, strings.TrimSpace(string(out)))
		}
		cloned = true
	} else {
		if out, err := runGit(ctx, env, "git", "-C", dir, "remote", "set-url", "origin", url); err != nil {
			return before, "", fmt.Errorf("git remote set-url: %w: %s", err, strings.TrimSpace(string(out)))
		}
		if out, err := runGit(ctx, env, gitArgs(c.InsecureSkipVerify, "git", "-C", dir, "fetch", "origin", branch)...); err != nil {
			return before, "", fmt.Errorf("git fetch: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}

	ref := "FETCH_HEAD"
	if cloned {
		ref = "origin/" + branch
	}
	for _, args := range [][]string{
		{"git", "-C", dir, "checkout", "-f", ref},
		{"git", "-C", dir, "reset", "--hard", ref},
		{"git", "-C", dir, "clean", "-fdx"},
	} {
		if out, err := runGit(ctx, env, args...); err != nil {
			return before, "", fmt.Errorf("%s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return before, head(ctx, dir), nil
}

func head(ctx context.Context, dir string) string {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return ""
	}
	out, err := runGit(ctx, nil, "git", "-C", dir, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (c Creds) env() (_ []string, cleanup func(), err error) {
	cleanup = func() {}
	if c.SSHKey != "" {
		f, err := os.CreateTemp("", "helios-config-repo-*")
		if err != nil {
			return nil, cleanup, fmt.Errorf("temp ssh key: %w", err)
		}
		known, err := os.CreateTemp("", "helios-known-hosts-*")
		if err != nil {
			f.Close()
			os.Remove(f.Name())
			return nil, cleanup, fmt.Errorf("temp known_hosts: %w", err)
		}
		knownPath := known.Name()
		if err := known.Close(); err != nil {
			f.Close()
			os.Remove(f.Name())
			os.Remove(knownPath)
			return nil, cleanup, err
		}
		cleanup = func() {
			os.Remove(f.Name())
			os.Remove(knownPath)
		}
		if _, err := f.WriteString(c.SSHKey); err != nil {
			f.Close()
			cleanup()
			return nil, cleanup, err
		}
		if err := f.Close(); err != nil {
			cleanup()
			return nil, cleanup, err
		}
		if err := os.Chmod(f.Name(), 0o600); err != nil {
			cleanup()
			return nil, cleanup, err
		}
		ssh := "ssh -i " + f.Name() + " -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=" + knownPath
		return []string{"GIT_SSH_COMMAND=" + ssh}, cleanup, nil
	}
	if c.Username == "" && c.Password == "" {
		return nil, cleanup, nil
	}
	user, pass := c.Username, c.Password
	if user == "" {
		user, pass = c.Password, ""
	}
	f, err := os.CreateTemp("", "helios-config-askpass-*")
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
	return cmd.CombinedOutput()
}
