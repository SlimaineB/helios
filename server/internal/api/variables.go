package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/slim/ansiblehub/server/internal/model"
)

// secretSentinel is what the operator API returns for a secret, and what a
// client sends back to mean "keep the stored value".
const secretSentinel = "********"

type varsPayload struct {
	Items []model.Variable `json:"items"`
}

func (s *Server) handleListAppVariables(w http.ResponseWriter, r *http.Request) {
	appID, ok := appIDParam(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid app id")
		return
	}
	vars, err := s.store.AppVariables(r.Context(), appID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, varsPayload{Items: redactVars(vars)})
}

func (s *Server) handleUpdateAppVariables(w http.ResponseWriter, r *http.Request) {
	appID, ok := appIDParam(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid app id")
		return
	}
	in, err := readVarsPayload(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	current, err := s.store.AppVariables(r.Context(), appID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.store.SaveAppVariables(r.Context(), appID, resolveVariables(in, current)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	saved, err := s.store.AppVariables(r.Context(), appID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, varsPayload{Items: redactVars(saved)})
}

func (s *Server) handleListAssignedVariables(w http.ResponseWriter, r *http.Request) {
	instID, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid instance id")
		return
	}
	appID, ok := appIDParam(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid app id")
		return
	}
	vars, err := s.store.AssignmentVariables(r.Context(), instID, appID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, varsPayload{Items: redactVars(vars)})
}

func (s *Server) handleUpdateAssignedVariables(w http.ResponseWriter, r *http.Request) {
	instID, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid instance id")
		return
	}
	appID, ok := appIDParam(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid app id")
		return
	}
	in, err := readVarsPayload(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	current, err := s.store.AssignmentVariables(r.Context(), instID, appID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.store.SaveAssignmentVariables(r.Context(), instID, appID, resolveVariables(in, current)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	saved, err := s.store.AssignmentVariables(r.Context(), instID, appID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, varsPayload{Items: redactVars(saved)})
}

func readVarsPayload(r *http.Request) ([]model.Variable, error) {
	var p varsPayload
	if err := readJSON(r, &p); err != nil {
		return nil, err
	}
	byKey := map[string]model.Variable{}
	for _, v := range p.Items {
		if v.Key == "" {
			continue
		}
		byKey[v.Key] = v // last occurrence wins
	}
	out := make([]model.Variable, 0, len(byKey))
	for _, v := range byKey {
		out = append(out, v)
	}
	return out, nil
}

// resolveVariables keeps the stored value when a secret is submitted empty or
// as the sentinel (write-only roundtrip). Plain variables pass through.
func resolveVariables(in, current []model.Variable) []model.Variable {
	cur := map[string]string{}
	for _, v := range current {
		cur[v.Key] = v.Value
	}
	out := make([]model.Variable, 0, len(in))
	for _, v := range in {
		if v.IsSecret && (v.Value == "" || v.Value == secretSentinel) {
			v.Value = cur[v.Key]
		}
		out = append(out, v)
	}
	return out
}

// redactVars blanks secret values before they leave the operator API.
func redactVars(vars []model.Variable) []model.Variable {
	out := make([]model.Variable, 0, len(vars))
	for _, v := range vars {
		if v.IsSecret {
			v.Value = secretSentinel
		}
		out = append(out, v)
	}
	return out
}

// mergedVars builds the extra-vars map sent to an agent for one assignment:
// legacy vars < app defaults < host overrides.
func (s *Server) mergedVars(ctx context.Context, instanceID string, appID int64, legacy string) (string, error) {
	out := map[string]any{}
	_ = json.Unmarshal([]byte(legacy), &out)

	appVars, err := s.store.AppVariables(ctx, appID)
	if err != nil {
		return "", err
	}
	hostVars, err := s.store.AssignmentVariables(ctx, instanceID, appID)
	if err != nil {
		return "", err
	}
	for _, vars := range [][]model.Variable{appVars, hostVars} {
		for _, v := range vars {
			out[v.Key] = parseVarValue(v.Value)
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// parseVarValue keeps values parseable as JSON typed (number, bool, list,
// object), everything else becomes a plain string.
func parseVarValue(s string) any {
	var v any
	if json.Unmarshal([]byte(s), &v) == nil {
		return v
	}
	return s
}
