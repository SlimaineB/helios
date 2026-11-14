package gitops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}

func TestIsCommitSHA(t *testing.T) {
	cases := map[string]bool{
		"fa60c7c4d81e6e716b0a1f56b0761b3b0cb5f8c9": true,
		"fa60c7c":    true,
		"FA60C7C":    true,
		"main":       false,
		"v1.2.3":     false,
		"fa60c7":     false,
		"fa60c7c4x":  false,
		"":           false,
		"release-42": false,
	}
	for in, want := range cases {
		if got := isCommitSHA(in); got != want {
			t.Errorf("isCommitSHA(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestOpenSyncChecksOutPinnedCommit(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	origin := filepath.Join(root, "origin")

	runGitTest(t, root, "init", "-q", origin)
	if err := os.WriteFile(filepath.Join(origin, "a.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, origin, "add", ".")
	runGitTest(t, origin, "commit", "-qm", "one")
	first := runGitTest(t, origin, "rev-parse", "HEAD")

	if err := os.WriteFile(filepath.Join(origin, "a.txt"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, origin, "commit", "-qam", "two")
	head := runGitTest(t, origin, "rev-parse", "HEAD")

	url := "file://" + origin
	mirror := filepath.Join(root, "mirror")

	// Fresh clone straight onto the pinned SHA.
	if _, after, err := OpenSync(ctx, mirror, url, first, Creds{}); err != nil {
		t.Fatalf("OpenSync pinned clone: %v", err)
	} else if after != first {
		t.Fatalf("after = %s, want %s", after, first)
	}

	// Existing mirror moving to another pinned SHA via the fetch path.
	if _, after, err := OpenSync(ctx, mirror, url, head, Creds{}); err != nil {
		t.Fatalf("OpenSync pinned fetch: %v", err)
	} else if after != head {
		t.Fatalf("after = %s, want %s", after, head)
	}

	// Abbreviated SHA must resolve too.
	if _, after, err := OpenSync(ctx, mirror, url, first[:8], Creds{}); err != nil {
		t.Fatalf("OpenSync abbreviated pin: %v", err)
	} else if after != first {
		t.Fatalf("after = %s, want %s", after, first)
	}
}
