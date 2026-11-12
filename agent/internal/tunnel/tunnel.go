package tunnel

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/slim/helios/agent/internal/client"
	"github.com/slim/helios/agent/internal/config"
)

const (
	defaultConnectGrace = 2 * time.Second
	defaultHealth       = 30 * time.Second
	defaultBackoffMin   = 5 * time.Second
	defaultBackoffMax   = time.Minute
)

type target struct {
	Gateway string
	Port    int
}

// Manager maintains a reverse SSH tunnel and reports its health to the hub.
type Manager struct {
	cfg         config.Config
	client      *client.Client
	log         *slog.Logger
	version     string
	hostname    string
	platform    string
	sshBin      string
	connectWait time.Duration
	backoffMin  time.Duration
	backoffMax  time.Duration
}

func New(cfg config.Config, c *client.Client, log *slog.Logger, version string) *Manager {
	host, _ := os.Hostname()
	if cfg.Hostname != "" {
		host = cfg.Hostname
	}
	platform := runtime.GOOS + "/" + runtime.GOARCH
	if cfg.Platform != "" {
		platform = cfg.Platform
	}
	return &Manager{
		cfg:         cfg,
		client:      c,
		log:         log,
		version:     version,
		hostname:    host,
		platform:    platform,
		sshBin:      "ssh",
		connectWait: defaultConnectGrace,
		backoffMin:  defaultBackoffMin,
		backoffMax:  defaultBackoffMax,
	}
}

func (m *Manager) Run(ctx context.Context) error {
	if key := strings.TrimSpace(m.cfg.TunnelSSHKey); key != "" {
		if _, err := os.Stat(key); err != nil {
			return fmt.Errorf("tunnel ssh key: %w", err)
		}
	}
	m.log.Info("tunnel agent starting", "server", m.cfg.Server, "hostname", m.hostname)
	backoff := m.backoffMin

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		t, err := m.target(ctx)
		if err != nil {
			m.log.Error("tunnel config unavailable", "error", err)
			m.report(ctx, false, err)
			if !sleep(ctx, backoff) {
				return ctx.Err()
			}
			backoff = nextBackoff(backoff, m.backoffMax)
			continue
		}

		wasConnected, err := m.runOnce(ctx, t)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			m.log.Warn("reverse tunnel stopped", "error", err)
			m.report(ctx, false, err)
		}
		if wasConnected {
			backoff = m.backoffMin
		} else {
			backoff = nextBackoff(backoff, m.backoffMax)
		}
		if !sleep(ctx, backoff) {
			return ctx.Err()
		}
	}
}

func (m *Manager) target(ctx context.Context) (target, error) {
	var t target
	if m.cfg.TunnelGateway != "" && m.cfg.TunnelPort > 0 {
		t = target{Gateway: m.cfg.TunnelGateway, Port: m.cfg.TunnelPort}
	} else {
		info, err := m.client.TunnelConfig(ctx)
		if err != nil {
			return t, err
		}
		t = target{Gateway: info.Gateway, Port: info.Port}
	}
	if m.cfg.TunnelGateway != "" {
		t.Gateway = m.cfg.TunnelGateway
	}
	if m.cfg.TunnelPort > 0 {
		t.Port = m.cfg.TunnelPort
	}
	t.Gateway = strings.TrimSpace(t.Gateway)
	if t.Gateway == "" {
		return t, fmt.Errorf("tunnel gateway is empty")
	}
	if t.Port < 1 || t.Port > 65535 {
		return t, fmt.Errorf("invalid tunnel port %d", t.Port)
	}
	return t, nil
}

func (m *Manager) runOnce(ctx context.Context, t target) (bool, error) {
	if err := os.MkdirAll(m.cfg.StateDir, 0o755); err != nil {
		return false, fmt.Errorf("state dir: %w", err)
	}
	knownHosts := filepath.Join(m.cfg.StateDir, "tunnel_known_hosts")
	args := sshArgs(m.cfg, t, knownHosts)

	m.log.Info("starting reverse tunnel", "gateway", t.Gateway, "port", t.Port, "target", localTarget(m.cfg))
	cmd := exec.CommandContext(ctx, m.sshBin, args...)
	lw := &logWriter{log: m.log}
	cmd.Stdout = lw
	cmd.Stderr = lw
	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("start ssh: %w", err)
	}

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	connectWait := m.connectWait
	if connectWait <= 0 {
		connectWait = defaultConnectGrace
	}
	healthEvery := m.cfg.TunnelHealthInterval
	if healthEvery <= 0 {
		healthEvery = defaultHealth
	}
	started := time.NewTimer(connectWait)
	defer started.Stop()
	ticker := time.NewTicker(healthEvery)
	defer ticker.Stop()

	connected := false
	for {
		select {
		case err := <-waitCh:
			if err != nil {
				return connected, fmt.Errorf("ssh exited: %w", err)
			}
			return connected, fmt.Errorf("ssh exited")
		case <-started.C:
			connected = true
			m.log.Info("reverse tunnel established", "gateway", t.Gateway, "port", t.Port)
			m.report(ctx, true, nil)
		case <-ticker.C:
			if connected {
				m.report(ctx, true, nil)
			}
		case <-ctx.Done():
			return connected, ctx.Err()
		}
	}
}

func (m *Manager) report(ctx context.Context, connected bool, cause error) {
	if ctx.Err() != nil {
		return
	}
	h := client.TunnelHealth{
		Connected: connected,
		Hostname:  m.hostname,
		Platform:  m.platform,
		Version:   m.version,
	}
	if cause != nil {
		h.Error = cause.Error()
	}
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := m.client.ReportTunnelHealth(rctx, h); err != nil {
		m.log.Warn("tunnel health report failed", "connected", connected, "error", err)
	}
}

func sshArgs(cfg config.Config, t target, knownHosts string) []string {
	args := []string{"-F", "/dev/null", "-N", "-T"}
	if extra := strings.TrimSpace(cfg.TunnelSSHCommonArgs); extra != "" {
		args = append(args, strings.Fields(extra)...)
	}
	args = append(args,
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=3",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile="+knownHosts,
	)
	if key := strings.TrimSpace(cfg.TunnelSSHKey); key != "" {
		args = append(args, "-o", "IdentitiesOnly=yes", "-i", key)
	}
	args = append(args,
		"-R", fmt.Sprintf("127.0.0.1:%d:%s", t.Port, localTarget(cfg)),
		t.Gateway,
	)
	return args
}

func localTarget(cfg config.Config) string {
	if v := strings.TrimSpace(cfg.TunnelLocalTarget); v != "" {
		return v
	}
	return "127.0.0.1:22"
}

func nextBackoff(current, max time.Duration) time.Duration {
	if current <= 0 {
		current = defaultBackoffMin
	}
	next := current * 2
	if max > 0 && next > max {
		return max
	}
	return next
}

func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		d = time.Second
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

type logWriter struct {
	log *slog.Logger
	buf []byte
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimSpace(string(w.buf[:i]))
		w.buf = w.buf[i+1:]
		if line != "" {
			w.log.Debug("ssh", "line", line)
		}
	}
	if len(w.buf) > 4096 {
		w.buf = w.buf[len(w.buf)-4096:]
	}
	return len(p), nil
}
