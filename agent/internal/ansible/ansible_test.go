package ansible

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slim/helios/agent/internal/gitops"
)

func TestRunPassesVarsAsExtraVars(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	bin := filepath.Join(dir, "fake-ansible")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ARGS_FILE\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	r := Runner{Bin: bin, ExtraEnv: map[string]string{"ARGS_FILE": argsFile}}
	if _, err := r.Check(context.Background(), &gitops.Repo{Dir: dir}, "site.yml", `{"env":"prod","replicas":2}`); err != nil {
		t.Fatalf("check: %v", err)
	}
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for i, arg := range args {
		if arg == "-e" {
			if i+1 >= len(args) || args[i+1] != `{"env":"prod","replicas":2}` {
				t.Fatalf("missing extra-vars value: %q", args)
			}
			return
		}
	}
	t.Fatalf("missing -e extra-vars argument: %q", args)
}

func TestRunOmitsEmptyExtraVars(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	bin := filepath.Join(dir, "fake-ansible")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ARGS_FILE\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	r := Runner{Bin: bin, ExtraEnv: map[string]string{"ARGS_FILE": argsFile}}
	if _, err := r.Apply(context.Background(), &gitops.Repo{Dir: dir}, "site.yml", "{}"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "-e") {
		t.Fatalf("empty vars should not be passed: %q", raw)
	}
}

func TestRunUsesSSHTargetWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	bin := filepath.Join(dir, "fake-ansible")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ARGS_FILE\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	r := Runner{
		Bin:      bin,
		ExtraEnv: map[string]string{"ARGS_FILE": argsFile},
		Target: &Target{
			Host:       "127.0.0.1",
			Port:       22000,
			User:       "tunnel",
			KeyFile:    "/etc/helios/runner_id",
			CommonArgs: "-o StrictHostKeyChecking=accept-new",
		},
	}
	if _, err := r.Check(context.Background(), &gitops.Repo{Dir: dir}, "site.yml", ""); err != nil {
		t.Fatalf("check: %v", err)
	}
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--connection ssh",
		"-i 127.0.0.1,",
		"-u tunnel",
		"--private-key /etc/helios/runner_id",
		"-e ansible_port=22000",
		"--ssh-common-args -o StrictHostKeyChecking=accept-new",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q: %q", want, args)
		}
	}
	if strings.Contains(joined, "--connection local") {
		t.Fatalf("remote target must not use local connection: %q", args)
	}
}
