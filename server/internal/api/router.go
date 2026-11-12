package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/slim/helios/server/internal/auth"
	"github.com/slim/helios/server/internal/metrics"
	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/store"
)

type ctxKey int

const (
	ctxInstance ctxKey = iota
	ctxToken
)

func (s *Server) routes() {
	m := s.mux

	// Health is public (probes).
	m.HandleFunc("GET /api/v1/health", s.handleHealth)
	m.HandleFunc("GET /metrics", metrics.Handler())

	// Agent tarballs are public so hosts can bootstrap without a hub login.
	m.HandleFunc("GET /downloads/{name}", s.handleDownload)

	// Operator authentication surface (public entry points).
	m.HandleFunc("GET /api/v1/auth/realm", s.handleRealm)
	m.HandleFunc("GET /api/v1/auth/login", s.handleLogin)
	m.HandleFunc("GET /api/v1/auth/callback", s.handleCallback)
	m.HandleFunc("GET /api/v1/auth/logout", s.handleLogout)
	m.HandleFunc("GET /api/v1/auth/me", s.handleMe)

	// Operator / UI — each route declares the RBAC operation it needs.
	m.HandleFunc("GET /api/v1/downloads", s.op("instances.get", s.handleListDownloads))
	m.HandleFunc("GET /api/v1/instances", s.op("instances.get", s.handleListInstances))
	m.HandleFunc("POST /api/v1/instances", s.op("instances.create", s.handleCreateInstance))
	m.HandleFunc("GET /api/v1/instances/{id}", s.op("instances.get", s.handleGetInstance))
	m.HandleFunc("DELETE /api/v1/instances/{id}", s.op("instances.delete", s.handleDeleteInstance))
	m.HandleFunc("POST /api/v1/instances/{id}/assign", s.op("instances.update", s.handleAssign))
	m.HandleFunc("DELETE /api/v1/instances/{id}/assign/{appId}", s.op("instances.update", s.handleUnassign))
	m.HandleFunc("PUT /api/v1/instances/{id}/assign/{appId}/autoheal", s.op("instances.update", s.handleSetAutoHeal))
	m.HandleFunc("PUT /api/v1/instances/{id}/assign/{appId}/revision", s.op("instances.update", s.handleSetRevision))
	m.HandleFunc("POST /api/v1/instances/{id}/{action}", s.op("commands.create", s.handleCreateInstanceCommand))
	m.HandleFunc("GET /api/v1/instances/{id}/runs", s.op("runs.get", s.handleListRuns))
	m.HandleFunc("GET /api/v1/runs/{runId}/log", s.op("runs.get", s.handleRunLog))

	m.HandleFunc("GET /api/v1/applications", s.op("applications.get", s.handleListApplications))
	m.HandleFunc("POST /api/v1/applications", s.op("applications.create", s.handleCreateApplication))
	m.HandleFunc("PUT /api/v1/applications/{appId}", s.op("applications.update", s.handleUpdateApplication))
	m.HandleFunc("DELETE /api/v1/applications/{appId}", s.op("applications.delete", s.handleDeleteApplication))
	m.HandleFunc("GET /api/v1/applications/{appId}/runs", s.op("runs.get", s.handleListApplicationRuns))

	// Variables (app defaults + per-assignment host overrides); secrets write-only.
	m.HandleFunc("GET /api/v1/applications/{appId}/variables", s.op("applications.get", s.handleListAppVariables))
	m.HandleFunc("PUT /api/v1/applications/{appId}/variables", s.op("applications.update", s.handleUpdateAppVariables))
	m.HandleFunc("GET /api/v1/instances/{id}/assign/{appId}/variables", s.op("instances.get", s.handleListAssignedVariables))
	m.HandleFunc("PUT /api/v1/instances/{id}/assign/{appId}/variables", s.op("instances.update", s.handleUpdateAssignedVariables))

	m.HandleFunc("GET /api/v1/audit", s.op("audit.get", s.handleListAudit))

	m.HandleFunc("GET /api/v1/projects", s.op("projects.get", s.handleListProjects))
	m.HandleFunc("POST /api/v1/projects", s.op("projects.create", s.handleCreateProject))
	m.HandleFunc("PUT /api/v1/projects/{projectId}", s.op("projects.update", s.handleUpdateProject))
	m.HandleFunc("DELETE /api/v1/projects/{projectId}", s.op("projects.delete", s.handleDeleteProject))

	m.HandleFunc("GET /api/v1/sync-windows", s.op("windows.get", s.handleListSyncWindows))
	m.HandleFunc("POST /api/v1/sync-windows", s.op("windows.create", s.handleCreateSyncWindow))
	m.HandleFunc("PUT /api/v1/sync-windows/{windowId}", s.op("windows.update", s.handleUpdateSyncWindow))
	m.HandleFunc("DELETE /api/v1/sync-windows/{windowId}", s.op("windows.delete", s.handleDeleteSyncWindow))

	m.HandleFunc("GET /api/v1/repositories", s.op("repositories.get", s.handleListRepositories))
	m.HandleFunc("POST /api/v1/repositories", s.op("repositories.create", s.handleCreateRepository))
	m.HandleFunc("PUT /api/v1/repositories/{repoId}", s.op("repositories.update", s.handleUpdateRepository))
	m.HandleFunc("DELETE /api/v1/repositories/{repoId}", s.op("repositories.delete", s.handleDeleteRepository))

	m.HandleFunc("GET /api/v1/config-repos", s.op("configrepos.get", s.handleListConfigRepos))
	m.HandleFunc("POST /api/v1/config-repos", s.op("configrepos.create", s.handleCreateConfigRepo))
	m.HandleFunc("PUT /api/v1/config-repos/{repoId}", s.op("configrepos.update", s.handleUpdateConfigRepo))
	m.HandleFunc("DELETE /api/v1/config-repos/{repoId}", s.op("configrepos.delete", s.handleDeleteConfigRepo))
	m.HandleFunc("POST /api/v1/config-repos/{repoId}/sync", s.op("configrepos.sync", s.handleSyncConfigRepo))

	// Agent (bearer-token authenticated, unchanged)
	m.HandleFunc("POST /api/v1/agent/checkin", s.agentAuth(s.handleCheckin))
	m.HandleFunc("GET /api/v1/agent/apps/{appId}/snapshot", s.agentAuth(s.handleAgentSnapshot))
	m.HandleFunc("POST /api/v1/agent/status", s.agentAuth(s.handleStatus))
	m.HandleFunc("POST /api/v1/agent/commands/{cmdId}/result", s.agentAuth(s.handleCommandResult))
	m.HandleFunc("GET /api/v1/agent/tunnel", s.agentAuth(s.handleAgentTunnelConfig))
	m.HandleFunc("POST /api/v1/agent/tunnel/health", s.agentAuth(s.handleAgentTunnelHealth))

	// Runner (bastion-side, shared bearer token). Reuses the agent handlers so
	// tunnel instances get the same drift/autoheal/command semantics.
	m.HandleFunc("GET /api/v1/runner/instances", s.runnerAuth(s.handleRunnerInstances))
	m.HandleFunc("POST /api/v1/runner/instances/{id}/checkin", s.runnerInstanceAuth(s.handleCheckin))
	m.HandleFunc("GET /api/v1/runner/instances/{id}/apps/{appId}/snapshot", s.runnerInstanceAuth(s.handleAgentSnapshot))
	m.HandleFunc("POST /api/v1/runner/instances/{id}/status", s.runnerInstanceAuth(s.handleStatus))
	m.HandleFunc("POST /api/v1/runner/instances/{id}/commands/{cmdId}/result", s.runnerInstanceAuth(s.handleCommandResult))
}

// agentAuth authenticates the agent bearer token and resolves the instance.
func (s *Server) agentAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			writeErr(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		token := strings.TrimPrefix(header, "Bearer ")
		inst, err := s.store.GetInstanceByToken(r.Context(), auth.Hash(token))
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeErr(w, http.StatusUnauthorized, "invalid token")
				return
			}
			s.internalError(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), ctxInstance, inst)
		ctx = context.WithValue(ctx, ctxToken, token)
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) handleCommandResult(w http.ResponseWriter, r *http.Request) {
	inst := r.Context().Value(ctxInstance).(model.Instance)
	cmdID, err := parseCmdID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid command id")
		return
	}
	var body struct {
		Status string `json:"status"`
		Result string `json:"result"`
		Commit string `json:"commit,omitempty"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.Status != model.CmdDone && body.Status != model.CmdFailed {
		writeErr(w, http.StatusBadRequest, "status must be done|failed")
		return
	}
	if err := s.store.CompleteCommand(r.Context(), inst.ID, cmdID, body.Status, body.Result, body.Commit); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "command not found")
			return
		}
		s.internalError(w, r, err)
		return
	}
	// A completed apply should refresh the check state afterwards.
	if body.Status == model.CmdDone {
		if err := s.refreshInstanceState(r.Context(), inst); err != nil {
			s.log.Warn("failed to refresh instance state", "error", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func parseCmdID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("cmdId"), 10, 64)
}
