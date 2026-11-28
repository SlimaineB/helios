package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/slim/helios/agent/internal/runner"
)

func main() {
	var (
		server        = flag.String("server", env("HELIOS_SERVER", ""), "Helios hub URL, e.g. https://hub.example.com")
		token         = flag.String("token", env("HELIOS_RUNNER_TOKEN", ""), "runner bearer token")
		stateDir      = flag.String("state-dir", env("HELIOS_RUNNER_STATE_DIR", "/var/lib/helios/runner"), "where repo mirrors and temp inventories live")
		poll          = flag.Duration("poll-interval", envDuration("HELIOS_RUNNER_POLL_INTERVAL", 10*time.Second), "heartbeat and job claim interval")
		tags          = flag.String("tags", env("HELIOS_RUNNER_TAGS", ""), "comma-separated job tags, e.g. eu,prod,linux")
		maxConcurrent = flag.Int("max-concurrent", envInt("HELIOS_RUNNER_MAX_CONCURRENT", 2), "parallel playbook runs on this runner")
		runnerID      = flag.String("runner-id", env("HELIOS_RUNNER_ID", ""), "stable runner identity (default: generated in state-dir)")
		sshUser       = flag.String("ssh-user", env("HELIOS_RUNNER_SSH_USER", ""), "default SSH user when the inventory does not set one")
		sshKey        = flag.String("ssh-key", env("HELIOS_RUNNER_SSH_KEY", ""), "SSH private key mounted on the runner")
		sshArgs       = flag.String("ssh-common-args", env("HELIOS_RUNNER_SSH_COMMON_ARGS", ""), "extra ssh(1) arguments for Ansible")
		ansibleBin    = flag.String("ansible-bin", env("HELIOS_RUNNER_ANSIBLE_BIN", "ansible-playbook"), "ansible-playbook binary")
		insecure      = flag.Bool("insecure", envBool("HELIOS_INSECURE"), "skip TLS verification (testing only)")
		debug         = flag.Bool("debug", false, "verbose logging")
	)
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	if *server == "" || *token == "" {
		log.Error("server and runner token are required")
		os.Exit(2)
	}
	if *sshUser == "" {
		if u := os.Getenv("USER"); u != "" {
			*sshUser = u
		}
	}

	cfg := runner.Config{
		Server:        *server,
		Token:         *token,
		StateDir:      *stateDir,
		PollInterval:  *poll,
		Tags:          splitTags(*tags),
		MaxConcurrent: *maxConcurrent,
		RunnerID:      *runnerID,
		SSHUser:       *sshUser,
		SSHKey:        *sshKey,
		SSHCommonArgs: *sshArgs,
		AnsibleBin:    *ansibleBin,
		Insecure:      *insecure,
	}
	r := runner.New(cfg, log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := r.Run(ctx); err != nil && ctx.Err() == nil {
		log.Error("runner stopped", "error", err)
		os.Exit(1)
	}
}

func splitTags(raw string) []string {
	var out []string
	for _, tag := range strings.Split(raw, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			out = append(out, tag)
		}
	}
	return out
}

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func envInt(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDuration(name string, def time.Duration) time.Duration {
	if v := os.Getenv(name); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func envBool(name string) bool {
	if v := os.Getenv(name); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return false
}
