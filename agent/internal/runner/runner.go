// Package runner implements the bastion-side execution loop for agentless
// instances. It discovers reverse-SSH tunnel instances from the hub, starts one
// agent loop per reachable instance, and reuses the exact same Ansible/git/drift
// logic as the installed agent.
package runner

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/slim/helios/agent/internal/client"
	"github.com/slim/helios/agent/internal/config"
	"github.com/slim/helios/agent/internal/loop"
)

type Config struct {
	Server            string
	Token             string
	StateDir          string
	PollInterval      time.Duration
	DiscoverInterval  time.Duration
	MaxConcurrentApps int
	SSHUser           string
	SSHKey            string
	SSHCommonArgs     string
	AnsibleBin        string
	Insecure          bool
}

type Runner struct {
	cfg     Config
	log     *slog.Logger
	control *client.Client

	mu     sync.Mutex
	agents map[string]context.CancelFunc
}

func New(cfg Config, log *slog.Logger) *Runner {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 30 * time.Second
	}
	if cfg.DiscoverInterval <= 0 {
		cfg.DiscoverInterval = 15 * time.Second
	}
	if cfg.MaxConcurrentApps <= 0 {
		cfg.MaxConcurrentApps = 4
	}
	if cfg.AnsibleBin == "" {
		cfg.AnsibleBin = "ansible-playbook"
	}
	if cfg.StateDir == "" {
		cfg.StateDir = "/var/lib/helios/runner"
	}
	return &Runner{
		cfg:     cfg,
		log:     log,
		control: client.New(cfg.Server, cfg.Token, cfg.Insecure),
		agents:  map[string]context.CancelFunc{},
	}
}

func (r *Runner) Run(ctx context.Context) error {
	if err := os.MkdirAll(r.cfg.StateDir, 0o755); err != nil {
		return err
	}
	r.log.Info("runner starting",
		"server", r.cfg.Server,
		"state_dir", r.cfg.StateDir,
		"poll", r.cfg.PollInterval.String(),
		"ssh_user", r.cfg.SSHUser)

	ticker := time.NewTicker(r.cfg.DiscoverInterval)
	defer ticker.Stop()
	for {
		r.reconcile(ctx)
		select {
		case <-ctx.Done():
			r.stopAll()
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (r *Runner) reconcile(ctx context.Context) {
	insts, err := r.control.ListTunnelInstances(ctx)
	if err != nil {
		r.log.Error("tunnel instance discovery failed", "error", err)
		return
	}
	seen := make(map[string]bool, len(insts))
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, inst := range insts {
		if inst.TunnelPort <= 0 {
			continue
		}
		seen[inst.ID] = true
		if _, ok := r.agents[inst.ID]; ok {
			continue
		}
		ictx, cancel := context.WithCancel(ctx)
		r.agents[inst.ID] = cancel
		go r.runInstance(ictx, inst)
	}
	for id, cancel := range r.agents {
		if !seen[id] {
			cancel()
			delete(r.agents, id)
		}
	}
}

func (r *Runner) stopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, cancel := range r.agents {
		cancel()
		delete(r.agents, id)
	}
}

func (r *Runner) runInstance(ctx context.Context, inst client.RunnerInstance) {
	log := r.log.With("instance", inst.Name, "instance_id", inst.ID, "port", inst.TunnelPort)
	cfg := config.Config{
		Server:            r.cfg.Server,
		Token:             r.cfg.Token,
		InstanceID:        inst.ID,
		StateDir:          filepath.Join(r.cfg.StateDir, inst.ID),
		PollInterval:      r.cfg.PollInterval,
		MaxConcurrentApps: r.cfg.MaxConcurrentApps,
		AnsibleBin:        r.cfg.AnsibleBin,
		TargetHost:        "127.0.0.1",
		TargetPort:        inst.TunnelPort,
		SSHUser:           r.cfg.SSHUser,
		SSHKey:            r.cfg.SSHKey,
		SSHCommonArgs:     r.cfg.SSHCommonArgs,
		Hostname:          inst.Name,
		Platform:          "reverse-ssh-tunnel",
	}
	cl := client.NewRunner(r.cfg.Server, r.cfg.Token, inst.ID, inst.TunnelPort, r.cfg.Insecure)
	agent := loop.New(cfg, cl, log)
	if err := agent.ResolveInstanceID(ctx); err != nil {
		log.Error("instance id resolution failed", "error", err)
		return
	}
	if err := agent.Run(ctx); err != nil && ctx.Err() == nil {
		log.Error("runner instance stopped", "error", err)
	}
}
