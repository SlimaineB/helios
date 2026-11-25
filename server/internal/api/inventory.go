package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/slim/helios/server/internal/gitops"
	"github.com/slim/helios/server/internal/inventory"
	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/repocache"
	"github.com/slim/helios/server/internal/store"
)

const redactedValue = "••••••"

type inventoryInstanceRef struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	InventoryHost string `json:"inventory_host"`
}

type applicationInventory struct {
	Enabled         bool                   `json:"enabled"`
	Source          string                 `json:"source"`
	InventorySource string                 `json:"inventory_source"`
	Editable        bool                   `json:"editable"`
	Commit          string                 `json:"commit,omitempty"`
	Groups          []inventory.Group      `json:"groups"`
	Warnings        []string               `json:"warnings"`
	Instances       []inventoryInstanceRef `json:"instances"`
}

// handleApplicationInventory parses the application's static Ansible inventory
// from the repo snapshot cache so operators can inspect it and pick hosts.
func (s *Server) handleApplicationInventory(w http.ResponseWriter, r *http.Request) {
	id, ok := appIDParam(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "missing app id")
		return
	}
	app, err := s.store.GetApplication(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "application not found")
			return
		}
		s.internalError(w, r, err)
		return
	}
	if !s.requireObject(w, r, "applications.get", app.Project) {
		return
	}

	out := applicationInventory{
		Source:          app.InventoryPath,
		InventorySource: app.InventorySource,
		Groups:          []inventory.Group{},
		Warnings:        []string{},
		Instances:       []inventoryInstanceRef{},
	}
	if out.InventorySource == "" {
		out.InventorySource = model.InventorySourceRepo
	}
	out.Editable = out.InventorySource == model.InventorySourceHub && s.allow(r, "applications.update", app.Project)
	if insts, err := s.store.ListRunnerInstances(r.Context()); err == nil {
		for _, inst := range insts {
			if inst.Project != app.Project {
				continue
			}
			out.Instances = append(out.Instances, inventoryInstanceRef{
				ID: inst.ID, Name: inst.Name, InventoryHost: inst.InventoryHost,
			})
		}
	}

	if out.InventorySource == model.InventorySourceHub {
		out.Enabled = true
		parsed, perr := inventory.ParseJSON(app.InventoryData)
		if perr != nil {
			out.Warnings = append(out.Warnings, "Stored inventory is invalid: "+perr.Error())
		} else {
			out.Groups = parsed.Groups
		}
		redactInventoryGroups(out.Groups)
		writeJSON(w, http.StatusOK, out)
		return
	}

	source := strings.TrimSpace(app.InventoryPath)
	if source == "" {
		out.Warnings = append(out.Warnings, "This application has no inventory_path. Set it in the application configuration to preview hosts.")
		writeJSON(w, http.StatusOK, out)
		return
	}
	if s.repoCache == nil {
		out.Warnings = append(out.Warnings, "The hub repo snapshot cache is disabled, so the inventory cannot be read.")
		writeJSON(w, http.StatusOK, out)
		return
	}
	out.Enabled = true
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(2 * time.Minute))

	creds := gitops.Creds{}
	if repo, err := s.store.GetRepositoryByProjectURL(r.Context(), app.Project, app.RepoURL); err == nil {
		creds = gitops.Creds{
			Username:           repo.Username,
			Password:           repo.Password,
			SSHKey:             repo.SSHKey,
			InsecureSkipVerify: repo.InsecureSkipVerify,
		}
	}
	snap, err := s.repoCache.Ensure(r.Context(), app.RepoURL, app.Branch, creds, "")
	if err != nil {
		if errors.Is(err, repocache.ErrUnsupportedRevision) {
			out.Warnings = append(out.Warnings, "This revision is a semver range and cannot be previewed by the repo cache.")
		} else {
			s.internalError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	files, err := repocache.ReadTree(snap, source)
	if err != nil {
		out.Warnings = append(out.Warnings, fmt.Sprintf("Cannot read %q: %v", source, err))
		writeJSON(w, http.StatusOK, out)
		return
	}
	if len(files) == 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf("No files found at %q in commit %s.", source, shortCommit(snap.Commit)))
		writeJSON(w, http.StatusOK, out)
		return
	}
	parsed := inventory.Parse(files)
	out.Groups = parsed.Groups
	out.Warnings = append(out.Warnings, parsed.Warnings...)
	out.Commit = snap.Commit
	redactInventoryGroups(out.Groups)
	writeJSON(w, http.StatusOK, out)
}

// handleUpdateApplicationInventory replaces the hub-managed inventory. Values
// shown as redacted are restored from the stored inventory so editing a
// preview never overwrites a secret with the placeholder.
func (s *Server) handleUpdateApplicationInventory(w http.ResponseWriter, r *http.Request) {
	id, ok := appIDParam(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "missing app id")
		return
	}
	app, err := s.store.GetApplication(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "application not found")
			return
		}
		s.internalError(w, r, err)
		return
	}
	if !s.requireObject(w, r, "applications.update", app.Project) {
		return
	}
	owners, err := s.managedOwners(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	applyManaged(&app.ManagedBy, &app.ManagedByRepoID, owners, gitops.KindApplication, app.Name)
	if app.ManagedBy != "" {
		writeErr(w, http.StatusConflict, "application is managed by GitOps; edit the config repository instead")
		return
	}
	var in inventory.Result
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	in = inventory.Normalize(in)
	if err := inventory.Validate(in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if old, perr := inventory.ParseJSON(app.InventoryData); perr == nil {
		restoreRedactedInventory(in, old)
	}
	data, err := inventory.MarshalJSON(in)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.store.SetApplicationInventory(r.Context(), app.ID, model.InventorySourceHub, data); err != nil {
		s.internalError(w, r, err)
		return
	}
	out := applicationInventory{
		Enabled:         true,
		Source:          "",
		InventorySource: model.InventorySourceHub,
		Editable:        true,
		Groups:          in.Groups,
		Warnings:        []string{},
		Instances:       []inventoryInstanceRef{},
	}
	if insts, err := s.store.ListRunnerInstances(r.Context()); err == nil {
		for _, inst := range insts {
			if inst.Project != app.Project {
				continue
			}
			out.Instances = append(out.Instances, inventoryInstanceRef{
				ID: inst.ID, Name: inst.Name, InventoryHost: inst.InventoryHost,
			})
		}
	}
	redactInventoryGroups(out.Groups)
	writeJSON(w, http.StatusOK, out)
}

func redactInventoryGroups(groups []inventory.Group) {
	for i := range groups {
		groups[i].Vars = redactInventoryVars(groups[i].Vars)
		for j := range groups[i].Hosts {
			groups[i].Hosts[j].Vars = redactInventoryVars(groups[i].Hosts[j].Vars)
		}
	}
}

func restoreRedactedInventory(next, old inventory.Result) {
	oldGroups := make(map[string]inventory.Group, len(old.Groups))
	for _, g := range old.Groups {
		oldGroups[g.Name] = g
	}
	for i := range next.Groups {
		g := &next.Groups[i]
		prev, ok := oldGroups[g.Name]
		if !ok {
			continue
		}
		restoreRedactedVars(g.Vars, prev.Vars)
		prevHosts := make(map[string]inventory.Host, len(prev.Hosts))
		for _, h := range prev.Hosts {
			prevHosts[h.Name] = h
		}
		for j := range g.Hosts {
			if ph, ok := prevHosts[g.Hosts[j].Name]; ok {
				restoreRedactedVars(g.Hosts[j].Vars, ph.Vars)
			}
		}
	}
}

func restoreRedactedVars(next, old map[string]any) {
	for key, value := range next {
		if text, ok := value.(string); ok && text == redactedValue {
			if prev, ok := old[key]; ok {
				next[key] = prev
			}
		}
	}
}

func shortCommit(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

func redactInventoryVars(vars map[string]any) map[string]any {
	if len(vars) == 0 {
		return vars
	}
	for key := range vars {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "password") || strings.Contains(lower, "passwd") ||
			strings.Contains(lower, "secret") || strings.Contains(lower, "token") {
			vars[key] = redactedValue
		}
	}
	return vars
}
