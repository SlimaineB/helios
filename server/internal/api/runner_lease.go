package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/store"
)

// runnerLeaseTTL is how long an instance stays owned by a runner without a
// renewal. Runners renew on every discovery tick, so a crashed runner is
// replaced within a few ticks.
const runnerLeaseTTL = 90 * time.Second

const runnerIDHeader = "X-Helios-Runner-ID"

func runnerLeaseName(instanceID string) string { return "runner:instance:" + instanceID }

// runnerID reads the runner identity from the request. It is scoped to the
// runner API and only used for leases, never for authorization.
func runnerID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.TrimSpace(r.Header.Get(runnerIDHeader))
	if id == "" || len(id) > 128 {
		writeErr(w, http.StatusBadRequest, "missing or invalid "+runnerIDHeader)
		return "", false
	}
	for _, c := range id {
		if !(c == '-' || c == '_' || c == '.' || c == ':' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			writeErr(w, http.StatusBadRequest, "invalid "+runnerIDHeader)
			return "", false
		}
	}
	return id, true
}

func (s *Server) runnerSSHInstance(w http.ResponseWriter, r *http.Request) (model.Instance, bool) {
	inst, err := s.store.GetInstance(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "instance not found")
			return model.Instance{}, false
		}
		s.internalError(w, r, err)
		return model.Instance{}, false
	}
	if inst.ExecutionMode != model.ModeSSH {
		writeErr(w, http.StatusForbidden, "instance is not in ssh mode")
		return model.Instance{}, false
	}
	return inst, true
}

// handleRunnerClaim leases an SSH instance to the calling runner. A runner
// must hold the lease before starting the instance loop.
func (s *Server) handleRunnerClaim(w http.ResponseWriter, r *http.Request) {
	owner, ok := runnerID(w, r)
	if !ok {
		return
	}
	inst, ok := s.runnerSSHInstance(w, r)
	if !ok {
		return
	}
	acquired, err := s.store.AcquireLease(r.Context(), runnerLeaseName(inst.ID), owner, runnerLeaseTTL)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if !acquired {
		writeErr(w, http.StatusConflict, "instance is leased by another runner")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"instance_id": inst.ID,
		"runner_id":   owner,
		"ttl_seconds": int(runnerLeaseTTL.Seconds()),
	})
}

// handleRunnerRenew extends the lease. A 409 means the lease was lost and the
// runner must stop working on the instance.
func (s *Server) handleRunnerRenew(w http.ResponseWriter, r *http.Request) {
	owner, ok := runnerID(w, r)
	if !ok {
		return
	}
	inst, ok := s.runnerSSHInstance(w, r)
	if !ok {
		return
	}
	if err := s.store.RenewLease(r.Context(), runnerLeaseName(inst.ID), owner, runnerLeaseTTL); err != nil {
		if errors.Is(err, store.ErrLeaseLost) {
			writeErr(w, http.StatusConflict, "lease lost")
			return
		}
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"instance_id": inst.ID,
		"runner_id":   owner,
		"ttl_seconds": int(runnerLeaseTTL.Seconds()),
	})
}

// handleRunnerRelease gives the lease back so another runner can pick the
// instance up immediately (graceful shutdown or reconfiguration).
func (s *Server) handleRunnerRelease(w http.ResponseWriter, r *http.Request) {
	owner, ok := runnerID(w, r)
	if !ok {
		return
	}
	inst, ok := s.runnerSSHInstance(w, r)
	if !ok {
		return
	}
	if err := s.store.ReleaseLease(r.Context(), runnerLeaseName(inst.ID), owner); err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
