package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/slim/helios/server/internal/auth"
	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/store"
)

// runnerAuth authenticates the bastion runner with its shared bearer token.
// An empty server-side token disables the runner API entirely.
func (s *Server) runnerAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.runnerTokenHash == "" {
			writeErr(w, http.StatusServiceUnavailable, "runner API disabled")
			return
		}
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			writeErr(w, http.StatusUnauthorized, "missing runner token")
			return
		}
		got := auth.Hash(strings.TrimPrefix(header, "Bearer "))
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.runnerTokenHash)) != 1 {
			writeErr(w, http.StatusUnauthorized, "invalid runner token")
			return
		}
		next(w, r)
	}
}

// runnerInstanceAuth authenticates the runner and resolves the target tunnel
// instance into the request context, so the agent handlers can be reused.
func (s *Server) runnerInstanceAuth(next http.HandlerFunc) http.HandlerFunc {
	return s.runnerAuth(func(w http.ResponseWriter, r *http.Request) {
		inst, err := s.store.GetInstance(r.Context(), r.PathValue("id"))
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeErr(w, http.StatusNotFound, "instance not found")
				return
			}
			s.internalError(w, r, err)
			return
		}
		if inst.ExecutionMode != model.ModeTunnel {
			writeErr(w, http.StatusForbidden, "instance is not in tunnel mode")
			return
		}
		ctx := context.WithValue(r.Context(), ctxInstance, inst)
		next(w, r.WithContext(ctx))
	})
}

// handleRunnerInstances lists agentless instances and their allocated loopback
// ports. The runner then probes each port and calls the per-instance endpoints.
func (s *Server) handleRunnerInstances(w http.ResponseWriter, r *http.Request) {
	insts, err := s.store.ListTunnelInstances(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	for i := range insts {
		insts[i] = s.decorateInstance(insts[i])
	}
	writeJSON(w, http.StatusOK, map[string]any{"instances": insts})
}

// decorateInstance adds non-persisted, server-derived fields for the UI.
func (s *Server) decorateInstance(inst model.Instance) model.Instance {
	if inst.ExecutionMode == model.ModeTunnel {
		inst.TunnelGateway = s.tunnelGateway
	}
	return inst
}
