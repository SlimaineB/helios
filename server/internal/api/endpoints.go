package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/slim/ansiblehub/server/internal/store"
)

// handleRunLog streams the full console log of a run.
func (s *Server) handleRunLog(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("runId")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid run id")
		return
	}
	run, err := s.store.GetRun(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "run not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":           run.ID,
		"instance_id":  run.InstanceID,
		"app_id":       run.AppID,
		"mode":         run.Mode,
		"status":       run.Status,
		"started_at":   run.StartedAt,
		"finished_at":  run.FinishedAt,
		"commit_after": run.CommitAfter,
		"commit_msg":   run.CommitMsg,
		"output":       run.Output,
		"output_text":  run.OutputText,
		"diff":         run.OutputDiff,
		"resources":    nonNil(run.Resources),
	})
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// autoHealBody controls the per-(instance, app) autoheal override.
type autoHealBody struct {
	Enabled *bool `json:"enabled"`
	// Inherit clears the override so the application default applies again.
	Inherit bool `json:"inherit,omitempty"`
}

// handleSetAutoHeal toggles auto-sync for one assignment.
func (s *Server) handleSetAutoHeal(w http.ResponseWriter, r *http.Request) {
	instID, _ := idParam(r, "id")
	appID, ok := appIDParam(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "missing app id")
		return
	}
	var body autoHealBody
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.Inherit {
		if err := s.store.ClearAssignmentAutoHeal(r.Context(), instID, appID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeErr(w, http.StatusNotFound, "assignment not found")
				return
			}
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		ok200(w)
		return
	}
	if body.Enabled == nil {
		writeErr(w, http.StatusBadRequest, "enabled or inherit is required")
		return
	}
	if err := s.store.SetAssignmentAutoHeal(r.Context(), instID, appID, *body.Enabled); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "assignment not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// (Re)arming autoheal clears a stuck flag: the operator is telling us the
	// playbook was fixed, so give the heal loop a fresh chance to converge.
	if *body.Enabled {
		if err := s.store.ResetHealState(r.Context(), instID, appID); err != nil {
			s.log.Warn("failed to clear stuck on rearm", "instance", instID, "app", appID, "error", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": *body.Enabled})

	// Re-evaluate instance state in case the policy change affects drift.
	inst, err := s.store.GetInstance(context.Background(), instID)
	if err == nil {
		_ = s.refreshInstanceState(r.Context(), inst)
	}
}
