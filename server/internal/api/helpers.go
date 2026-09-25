package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/slim/ansiblehub/server/internal/model"
)

type errorBody struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func ok200(w http.ResponseWriter) { w.WriteHeader(http.StatusOK) }

// instanceListEnvelope carries each instance plus its apps for the dashboard.
type InstanceWithApps struct {
	model.Instance
	Apps []AppStatus `json:"apps"`
}

type AppStatus struct {
	Application model.Application `json:"application"`
	AutoHeal    bool              `json:"autoheal"`     // effective value (override or app default)
	AutoHealSet bool              `json:"autoheal_set"` // true when an explicit override exists
	LatestCheck model.Run         `json:"latest_check"`
	LatestApply model.Run         `json:"latest_apply,omitempty"`
	HasCheck    bool              `json:"has_check"`
	LastCheckAt time.Time         `json:"last_check_at,omitempty"`
}
