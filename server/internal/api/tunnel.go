package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/slim/helios/server/internal/model"
)

type tunnelConfigResponse struct {
	InstanceID string `json:"instance_id"`
	Name       string `json:"name"`
	Gateway    string `json:"gateway"`
	Port       int    `json:"port"`
}

type tunnelHealthPayload struct {
	Connected bool   `json:"connected"`
	Hostname  string `json:"hostname,omitempty"`
	Platform  string `json:"platform,omitempty"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (s *Server) handleAgentTunnelConfig(w http.ResponseWriter, r *http.Request) {
	inst := r.Context().Value(ctxInstance).(model.Instance)
	if inst.ExecutionMode != model.ModeTunnel {
		writeErr(w, http.StatusForbidden, "instance is not in tunnel mode")
		return
	}
	if strings.TrimSpace(s.tunnelGateway) == "" {
		writeErr(w, http.StatusServiceUnavailable, "tunnel gateway is not configured")
		return
	}
	if inst.TunnelPort <= 0 {
		writeErr(w, http.StatusConflict, "instance has no tunnel port")
		return
	}
	writeJSON(w, http.StatusOK, tunnelConfigResponse{
		InstanceID: inst.ID,
		Name:       inst.Name,
		Gateway:    s.tunnelGateway,
		Port:       inst.TunnelPort,
	})
}

func (s *Server) handleAgentTunnelHealth(w http.ResponseWriter, r *http.Request) {
	inst := r.Context().Value(ctxInstance).(model.Instance)
	if inst.ExecutionMode != model.ModeTunnel {
		writeErr(w, http.StatusForbidden, "instance is not in tunnel mode")
		return
	}
	var body tunnelHealthPayload
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.Connected {
		hostname := inst.Hostname
		if body.Hostname != "" {
			hostname = body.Hostname
		}
		platform := inst.Platform
		if body.Platform != "" {
			platform = body.Platform
		}
		version := inst.Version
		if body.Version != "" {
			version = body.Version
		}
		if err := s.store.TouchInstance(r.Context(), inst.ID, version, hostname, platform,
			0, time.Now().UTC(), true); err != nil {
			s.internalError(w, r, err)
			return
		}
	} else {
		s.log.Warn("tunnel agent reported disconnected", "instance", inst.ID, "error", body.Error)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
