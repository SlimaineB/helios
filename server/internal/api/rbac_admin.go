package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/projects"
	"github.com/slim/helios/server/internal/rbac"
	"github.com/slim/helios/server/internal/store"
)

// currentRBAC returns the live policy set. It is reloaded from the database on
// every RBAC mutation; the YAML file is only a first-run bootstrap.
func (s *Server) currentRBAC() *rbac.RBAC {
	s.rbacMu.RLock()
	defer s.rbacMu.RUnlock()
	if s.rbac != nil {
		return s.rbac
	}
	if s.opAuth != nil {
		return s.opAuth.RBAC
	}
	return nil
}

func (s *Server) rbacSource(ctx context.Context) string {
	policies, bindings, err := s.store.CountRBACConfig(ctx)
	if err == nil && (policies > 0 || bindings > 0) {
		return "db"
	}
	if s.opAuth != nil && s.opAuth.RBAC != nil {
		return "file"
	}
	return "builtin"
}

// ReloadRBAC is the exported startup/mutation reload entrypoint.
func (s *Server) ReloadRBAC(ctx context.Context) error { return s.reloadRBAC(ctx) }

// reloadRBAC rebuilds the in-memory matcher from the database when DB rows
// exist. With an empty DB it keeps the file/bootstrap policy.
func (s *Server) reloadRBAC(ctx context.Context) error {
	policies, err := s.store.ListRBACPolicies(ctx)
	if err != nil {
		return err
	}
	bindings, err := s.store.ListRBACBindings(ctx)
	if err != nil {
		return err
	}
	if len(policies) == 0 && len(bindings) == 0 {
		return nil
	}
	cfg := rbac.Config{Policies: make([]string, 0, len(policies)), Bindings: make([]string, 0, len(bindings))}
	for _, p := range policies {
		cfg.Policies = append(cfg.Policies, fmt.Sprintf("p, %s, %s, %s, %s", p.Role, p.Resource, p.Action, p.Object))
	}
	for _, b := range bindings {
		cfg.Bindings = append(cfg.Bindings, fmt.Sprintf("g, %s, %s", b.Group, b.Role))
	}
	rb, err := rbac.New(cfg)
	if err != nil {
		return err
	}
	s.rbacMu.Lock()
	s.rbac = rb
	s.rbacMu.Unlock()
	return nil
}

type rbacResponse struct {
	Source    string              `json:"source"`
	Policies  []model.RBACPolicy  `json:"policies"`
	Bindings  []model.RBACBinding `json:"bindings"`
	Resources []string            `json:"resources"`
	Actions   []string            `json:"actions"`
}

func (s *Server) handleGetRBAC(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	policies, err := s.store.ListRBACPolicies(ctx)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	bindings, err := s.store.ListRBACBindings(ctx)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rbacResponse{
		Source:    s.rbacSource(ctx),
		Policies:  policies,
		Bindings:  bindings,
		Resources: rbac.AllResources,
		Actions:   rbac.AllActions,
	})
}

type rbacPolicyInput struct {
	Role     string `json:"role"`
	Resource string `json:"resource"`
	Action   string `json:"action"`
	Object   string `json:"object"`
}

type rbacBindingInput struct {
	Group string `json:"group"`
	Role  string `json:"role"`
}

func cleanRBACValue(v string) string { return strings.TrimSpace(v) }

func validRBACValue(v string) bool {
	return v != "" && !strings.Contains(v, ",") && strings.TrimSpace(v) == v
}

func validRBACResource(v string) bool {
	if v == "*" {
		return true
	}
	for _, r := range rbac.AllResources {
		if r == v {
			return true
		}
	}
	return false
}

func validRBACAction(v string) bool {
	if v == "*" {
		return true
	}
	for _, a := range rbac.AllActions {
		if a == v {
			return true
		}
	}
	return false
}

func validRBACObject(v string) bool {
	if v == "*" {
		return true
	}
	return projects.ValidName(v) && v != projects.Global
}

func (s *Server) handleCreateRBACPolicy(w http.ResponseWriter, r *http.Request) {
	var in rbacPolicyInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	in.Role, in.Resource, in.Action, in.Object = cleanRBACValue(in.Role), cleanRBACValue(in.Resource), cleanRBACValue(in.Action), cleanRBACValue(in.Object)
	if in.Object == "" {
		in.Object = "*"
	}
	if !validRBACValue(in.Role) || !validRBACValue(in.Resource) || !validRBACValue(in.Action) || !validRBACValue(in.Object) {
		writeErr(w, http.StatusBadRequest, "role/resource/action/object must be non-empty and comma-free")
		return
	}
	if !validRBACResource(in.Resource) {
		writeErr(w, http.StatusBadRequest, "unknown resource (use one of the listed resources or *)")
		return
	}
	if !validRBACAction(in.Action) {
		writeErr(w, http.StatusBadRequest, "unknown action (use one of the listed actions or *)")
		return
	}
	if !validRBACObject(in.Object) {
		writeErr(w, http.StatusBadRequest, "object must be * or a valid project name")
		return
	}
	created, err := s.store.CreateRBACPolicy(r.Context(), model.RBACPolicy{
		Role: in.Role, Resource: in.Resource, Action: in.Action, Object: in.Object,
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeErr(w, http.StatusConflict, "policy already exists")
			return
		}
		s.internalError(w, r, err)
		return
	}
	if err := s.reloadRBAC(r.Context()); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.publishEvent(hubEvent{Type: "rbac", Status: "policy-created"})
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleDeleteRBACPolicy(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.DeleteRBACPolicy(r.Context(), id); err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.reloadRBAC(r.Context()); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.publishEvent(hubEvent{Type: "rbac", Status: "policy-deleted"})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

func (s *Server) handleCreateRBACBinding(w http.ResponseWriter, r *http.Request) {
	var in rbacBindingInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	in.Group, in.Role = cleanRBACValue(in.Group), cleanRBACValue(in.Role)
	if !validRBACValue(in.Group) || !validRBACValue(in.Role) {
		writeErr(w, http.StatusBadRequest, "group and role must be non-empty and comma-free")
		return
	}
	created, err := s.store.CreateRBACBinding(r.Context(), model.RBACBinding{Group: in.Group, Role: in.Role})
	if err != nil {
		if errors.Is(err, store.ErrAlreadyBound) {
			writeErr(w, http.StatusConflict, "group is already bound to this role")
			return
		}
		s.internalError(w, r, err)
		return
	}
	if err := s.reloadRBAC(r.Context()); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.publishEvent(hubEvent{Type: "rbac", Status: "binding-created"})
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleDeleteRBACBinding(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	bindings, err := s.store.ListRBACBindings(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	var target *model.RBACBinding
	for i := range bindings {
		if bindings[i].ID == id {
			target = &bindings[i]
			break
		}
	}
	if target == nil {
		writeErr(w, http.StatusNotFound, "binding not found")
		return
	}
	// Never let an OIDC-only deployment delete its last role:admin binding.
	if target.Role == "role:admin" && (s.opAuth == nil || s.opAuth.Basic == nil) {
		adminBindings := 0
		for _, b := range bindings {
			if b.Role == "role:admin" {
				adminBindings++
			}
		}
		if adminBindings <= 1 {
			writeErr(w, http.StatusBadRequest, "refusing to delete the last role:admin binding while Basic auth is disabled")
			return
		}
	}
	if err := s.store.DeleteRBACBinding(r.Context(), id); err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.reloadRBAC(r.Context()); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.publishEvent(hubEvent{Type: "rbac", Status: "binding-deleted"})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

// handleResetRBAC clears the DB-managed rules and falls back to the bootstrap
// file/built-ins. Useful after importing a file once.
func (s *Server) handleResetRBAC(w http.ResponseWriter, r *http.Request) {
	if err := s.store.ReplaceRBACConfig(r.Context(), model.RBACConfig{}); err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.reloadRBAC(r.Context()); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.publishEvent(hubEvent{Type: "rbac", Status: "reset"})
	s.handleGetRBAC(w, r)
}
