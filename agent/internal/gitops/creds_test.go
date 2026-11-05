package gitops

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestHTTPCredsUseAskPass(t *testing.T) {
	c := Creds{Username: "bot", Password: "s3cret"}
	env, cleanup, err := c.env()
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	defer cleanup()

	var askpass string
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_ASKPASS=") {
			askpass = strings.TrimPrefix(kv, "GIT_ASKPASS=")
		}
		if strings.Contains(kv, "s3cret") && !strings.HasPrefix(kv, "HELIOS_GIT_PASSWORD=") {
			t.Fatalf("password leaked outside the askpass env var: %q", kv)
		}
	}
	if askpass == "" {
		t.Fatalf("no GIT_ASKPASS in env: %v", env)
	}

	run := func(prompt string) string {
		cmd := exec.Command(askpass, prompt)
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("askpass %q: %v", prompt, err)
		}
		return strings.TrimSpace(string(out))
	}
	if got := run("Username for 'https://example.com': "); got != "bot" {
		t.Fatalf("username = %q, want bot", got)
	}
	if got := run("Password for 'https://bot@example.com': "); got != "s3cret" {
		t.Fatalf("password = %q, want s3cret", got)
	}
}

func TestSSHKeyEnvAndCleanup(t *testing.T) {
	c := Creds{SSHKey: "-----BEGIN KEY-----\nabc\n-----END KEY-----\n"}
	env, cleanup, err := c.env()
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	var keyPath string
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_SSH_COMMAND=") {
			fields := strings.Fields(strings.TrimPrefix(kv, "GIT_SSH_COMMAND="))
			for i, f := range fields {
				if f == "-i" && i+1 < len(fields) {
					keyPath = fields[i+1]
				}
			}
		}
	}
	if keyPath == "" {
		t.Fatalf("no -i key in GIT_SSH_COMMAND: %v", env)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("key file missing: %v", err)
	}
	cleanup()
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatalf("key file not cleaned up: %v", err)
	}
}
