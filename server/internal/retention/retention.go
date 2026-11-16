// Package retention prunes old run history, completed commands and audit
// entries. It is optional: every TTL may be disabled with 0.
package retention

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/slim/helios/server/internal/store"
)

// Config controls how long each record type is kept. Zero disables that TTL.
type Config struct {
	Runs     time.Duration
	Commands time.Duration
	Audit    time.Duration
	Interval time.Duration
}

// Stats is a snapshot of the latest pruning pass.
type Stats struct {
	LastRun         time.Time `json:"last_run,omitempty"`
	DeletedRuns     int64     `json:"deleted_runs"`
	DeletedCommands int64     `json:"deleted_commands"`
	DeletedAudit    int64     `json:"deleted_audit"`
	Error           string    `json:"error,omitempty"`
}

// Manager runs retention on a fixed interval.
type Manager struct {
	st  store.Store
	log *slog.Logger
	cfg Config

	mu    sync.RWMutex
	stats Stats
}

func New(st store.Store, log *slog.Logger, cfg Config) *Manager {
	if cfg.Interval <= 0 {
		cfg.Interval = 6 * time.Hour
	}
	return &Manager{st: st, log: log, cfg: cfg}
}

func (m *Manager) Config() Config { return m.cfg }

func (m *Manager) Stats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stats
}

// Start runs one pass immediately and then on the configured interval until
// ctx is cancelled.
func (m *Manager) Start(ctx context.Context) {
	if m == nil || (m.cfg.Runs <= 0 && m.cfg.Commands <= 0 && m.cfg.Audit <= 0) {
		return
	}
	m.RunOnce(ctx)
	ticker := time.NewTicker(m.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.RunOnce(ctx)
		}
	}
}

// RunOnce prunes every enabled record type and updates Stats.
func (m *Manager) RunOnce(ctx context.Context) Stats {
	now := time.Now().UTC()
	st := Stats{LastRun: now}

	var errs []error
	if m.cfg.Runs > 0 {
		n, err := m.st.PruneRuns(ctx, now.Add(-m.cfg.Runs))
		st.DeletedRuns = n
		if err != nil {
			errs = append(errs, err)
		}
	}
	if m.cfg.Commands > 0 {
		n, err := m.st.PruneCommands(ctx, now.Add(-m.cfg.Commands))
		st.DeletedCommands = n
		if err != nil {
			errs = append(errs, err)
		}
	}
	if m.cfg.Audit > 0 {
		n, err := m.st.PruneAudit(ctx, now.Add(-m.cfg.Audit))
		st.DeletedAudit = n
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		st.Error = errs[0].Error()
		if m.log != nil {
			m.log.Warn("retention pass failed", "error", st.Error)
		}
	}
	if m.log != nil && (st.DeletedRuns+st.DeletedCommands+st.DeletedAudit) > 0 {
		m.log.Info("retention pruned records",
			"runs", st.DeletedRuns, "commands", st.DeletedCommands, "audit", st.DeletedAudit)
	}
	m.mu.Lock()
	m.stats = st
	m.mu.Unlock()
	return st
}
