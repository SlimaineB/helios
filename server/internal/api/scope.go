package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/projects"
	"github.com/slim/helios/server/internal/store"
)

func (s *Server) principal(r *http.Request) *principal {
	pr, _ := r.Context().Value(ctxPrincipal).(*principal)
	return pr
}

// allow reports whether the caller may run operation within object (project).
func (s *Server) allow(r *http.Request, operation, object string) bool {
	if s.opAuth == nil || s.opAuth.RBAC == nil {
		return true // dev mode: no operator auth configured
	}
	pr := s.principal(r)
	if pr == nil {
		return false
	}
	res, act, _ := strings.Cut(operation, ".")
	return s.opAuth.RBAC.Authorize(pr.Groups, pr.Roles, res, act, object)
}

// requireObject checks a project scope and writes 403 on failure.
func (s *Server) requireObject(w http.ResponseWriter, r *http.Request, operation, object string) bool {
	if s.allow(r, operation, object) {
		return true
	}
	writeErr(w, http.StatusForbidden, "forbidden: project scope "+object)
	return false
}

// requireGlobal checks an operation against the global scope (project "*").
func (s *Server) requireGlobal(w http.ResponseWriter, r *http.Request, operation string) bool {
	return s.requireObject(w, r, operation, "")
}

// visibleProjects returns the projects a caller may read for a resource.
func (s *Server) visibleProjects(r *http.Request, resource, action string) (bool, []string) {
	if s.opAuth == nil || s.opAuth.RBAC == nil {
		return true, nil // dev mode
	}
	pr := s.principal(r)
	if pr == nil {
		return false, nil
	}
	return s.opAuth.RBAC.VisibleObjects(pr.Groups, pr.Roles, resource, action)
}

// validateAppProject ensures a project exists and that its repository
// allowlist accepts repoURL. Empty project = global (no restriction).
func (s *Server) validateAppProject(ctx context.Context, name, repoURL string) (string, error) {
	if name == projects.Global {
		return "", nil
	}
	if !projects.ValidName(name) {
		return "invalid project name", nil
	}
	p, err := s.store.GetProjectByName(ctx, name)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "unknown project: " + name, nil
		}
		return "", err
	}
	if !projects.MatchRepo(p.RepoPatterns, repoURL) {
		return "repository is not allowed by project " + name, nil
	}
	return "", nil
}

// validateProjectOnly ensures a project exists (for instances).
func (s *Server) validateProjectOnly(ctx context.Context, name string) (string, error) {
	if name == projects.Global {
		return "", nil
	}
	if !projects.ValidName(name) {
		return "invalid project name", nil
	}
	if _, err := s.store.GetProjectByName(ctx, name); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "unknown project: " + name, nil
		}
		return "", err
	}
	return "", nil
}

// ---- projects (operator) ----

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "projects.get") {
		return
	}
	out, err := s.store.ListProjects(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "projects.create") {
		return
	}
	var in model.Project
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if !projects.ValidName(in.Name) || in.Name == projects.Global {
		writeErr(w, http.StatusBadRequest, "invalid project name")
		return
	}
	out, err := s.store.CreateProject(r.Context(), in)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || errors.Is(err, store.ErrConflict) {
			writeErr(w, http.StatusConflict, "project already exists")
			return
		}
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "projects.update") {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("projectId"), 10, 64)
	if err != nil || id == 0 {
		writeErr(w, http.StatusBadRequest, "invalid project id")
		return
	}
	var in model.Project
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if !projects.ValidName(in.Name) || in.Name == projects.Global {
		writeErr(w, http.StatusBadRequest, "invalid project name")
		return
	}
	in.ID = id
	if err := s.store.UpdateProject(r.Context(), in); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "project not found")
			return
		}
		if errors.Is(err, store.ErrConflict) || strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, http.StatusConflict, "project name already exists")
			return
		}
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, in)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "projects.delete") {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("projectId"), 10, 64)
	if err != nil || id == 0 {
		writeErr(w, http.StatusBadRequest, "invalid project id")
		return
	}
	if err := s.store.DeleteProject(r.Context(), id); err != nil {
		s.internalError(w, r, err)
		return
	}
	ok200(w)
}

// requireInstanceScope loads an instance and enforces operation on its project.
func (s *Server) requireInstanceScope(w http.ResponseWriter, r *http.Request, id, operation string) bool {
	inst, err := s.store.GetInstance(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "instance not found")
		return false
	}
	return s.requireObject(w, r, operation, inst.Project)
}

// requireAppScope loads an application and enforces operation on its project.
func (s *Server) requireAppScope(w http.ResponseWriter, r *http.Request, id int64, operation string) bool {
	app, err := s.store.GetApplication(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "application not found")
		return false
	}
	return s.requireObject(w, r, operation, app.Project)
}
