package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/store"
)

const (
	maxRegionName        = 64
	maxRegionDescription = 500
)

func normalizeRegion(in *model.Region) string {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if in.Name == "" {
		return "name is required"
	}
	if utf8.RuneCountInString(in.Name) > maxRegionName {
		return "name is too long (max 64 characters)"
	}
	if strings.ContainsAny(in.Name, "\r\n\t") {
		return "name must be a single line"
	}
	if utf8.RuneCountInString(in.Description) > maxRegionDescription {
		return "description is too long (max 500 characters)"
	}
	return ""
}

func (s *Server) handleListRegions(w http.ResponseWriter, r *http.Request) {
	out, err := s.store.ListRegions(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateRegion(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "regions.create") {
		return
	}
	var in model.Region
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if msg := normalizeRegion(&in); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	out, err := s.store.CreateRegion(r.Context(), in)
	if err != nil {
		if errors.Is(err, store.ErrConflict) || strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, http.StatusConflict, "region already exists")
			return
		}
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleUpdateRegion(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "regions.update") {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("regionId"), 10, 64)
	if err != nil || id == 0 {
		writeErr(w, http.StatusBadRequest, "invalid region id")
		return
	}
	var in model.Region
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if msg := normalizeRegion(&in); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	in.ID = id
	if err := s.store.UpdateRegion(r.Context(), in); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "region not found")
			return
		}
		if errors.Is(err, store.ErrConflict) || strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, http.StatusConflict, "region already exists")
			return
		}
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, in)
}

func (s *Server) handleDeleteRegion(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "regions.delete") {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("regionId"), 10, 64)
	if err != nil || id == 0 {
		writeErr(w, http.StatusBadRequest, "invalid region id")
		return
	}
	if _, err := s.store.GetRegionByID(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "region not found")
			return
		}
		s.internalError(w, r, err)
		return
	}
	if err := s.store.DeleteRegion(r.Context(), id); err != nil {
		s.internalError(w, r, err)
		return
	}
	ok200(w)
}
