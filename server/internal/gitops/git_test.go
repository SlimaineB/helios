package gitops

import (
	"os"
	"strings"
	"testing"
)

func TestSSHCredsUsePrivateTempKnownHosts(t *testing.T) {
	var keyPath, knownHostsPath string
	creds := Creds{SSHKey: "test-key\n"}
	env, cleanup, err := creds.env()
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	for _, value := range env {
		if !strings.HasPrefix(value, "GIT_SSH_COMMAND=") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(value, "GIT_SSH_COMMAND="))
		for i, field := range fields {
			if strings.HasPrefix(field, "UserKnownHostsFile=") {
				knownHostsPath = strings.TrimPrefix(field, "UserKnownHostsFile=")
			}
			if field == "-i" && i+1 < len(fields) {
				keyPath = fields[i+1]
			}
		}
	}
	if keyPath == "" || knownHostsPath == "" {
		cleanup()
		t.Fatalf("missing temp paths in GIT_SSH_COMMAND: %v", env)
	}
	for _, path := range []string{keyPath, knownHostsPath} {
		info, err := os.Stat(path)
		if err != nil {
			cleanup()
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			cleanup()
			t.Fatalf("%s mode = %o, want private", path, info.Mode().Perm())
		}
	}
	cleanup()
	for _, path := range []string{keyPath, knownHostsPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still exists after cleanup: %v", path, err)
		}
	}
}
