package api

import (
	"errors"
	"net/http"

	"github.com/slim/helios/server/internal/metrics"
	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/store"
)

type bulkCommandError struct {
	InstanceID string `json:"instance_id"`
	Error      string `json:"error"`
}

type bulkCommandResult struct {
	Queued  int                `json:"queued"`
	Skipped int                `json:"skipped"`
	Errors  []bulkCommandError `json:"errors,omitempty"`
}

// handleBulkCommands queues a check or apply for every application assigned to
// the selected instances. It is intentionally best-effort: one forbidden or
// missing host must not prevent the rest of the batch from being queued.
func (s *Server) handleBulkCommands(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action      string   `json:"action"`
		InstanceIDs []string `json:"instance_ids"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.Action != model.ActionCheck && body.Action != model.ActionApply {
		writeErr(w, http.StatusBadRequest, "action must be check|apply")
		return
	}
	if len(body.InstanceIDs) == 0 {
		writeErr(w, http.StatusBadRequest, "instance_ids is required")
		return
	}
	if len(body.InstanceIDs) > 200 {
		writeErr(w, http.StatusBadRequest, "at most 200 instances per batch")
		return
	}
	seen := make(map[string]bool, len(body.InstanceIDs))
	out := bulkCommandResult{}
	for _, id := range body.InstanceIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		inst, err := s.store.GetInstance(r.Context(), id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				out.Errors = append(out.Errors, bulkCommandError{InstanceID: id, Error: "instance not found"})
				continue
			}
			out.Errors = append(out.Errors, bulkCommandError{InstanceID: id, Error: err.Error()})
			continue
		}
		if !s.allow(r, "commands.create", inst.Project) {
			out.Errors = append(out.Errors, bulkCommandError{InstanceID: id, Error: "forbidden: project scope " + inst.Project})
			continue
		}
		assignments, err := s.store.ListAssignments(r.Context(), id)
		if err != nil {
			out.Errors = append(out.Errors, bulkCommandError{InstanceID: id, Error: err.Error()})
			continue
		}
		if len(assignments) == 0 {
			out.Skipped++
			continue
		}
		for _, a := range assignments {
			if body.Action == model.ActionApply && s.frozen(r.Context(), a.AppID) {
				metrics.Frozen(a.AppID)
				out.Skipped++
				continue
			}
			if _, err := s.store.CreateCommand(r.Context(), model.Command{
				InstanceID: id,
				AppID:      a.AppID,
				Action:     body.Action,
			}); err != nil {
				out.Errors = append(out.Errors, bulkCommandError{InstanceID: id, Error: err.Error()})
				break
			}
			out.Queued++
		}
		s.publishEvent(hubEvent{Type: "command", InstanceID: id, Mode: body.Action, Status: model.CmdPending})
	}
	writeJSON(w, http.StatusOK, out)
}
