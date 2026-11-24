package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/slim/helios/server/internal/gitops"
	"github.com/slim/helios/server/internal/inventory"
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
	Enabled   bool                   `json:"enabled"`
	Source    string                 `json:"source"`
	Commit    string                 `json:"commit,omitempty"`
	Groups    []inventory.Group      `json:"groups"`
	Warnings  []string               `json:"warnings"`
	Instances []inventoryInstanceRef `json:"instances"`
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
		Source:    app.InventoryPath,
		Groups:    []inventory.Group{},
		Warnings:  []string{},
		Instances: []inventoryInstanceRef{},
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
	for i := range out.Groups {
		out.Groups[i].Vars = redactInventoryVars(out.Groups[i].Vars)
		for j := range out.Groups[i].Hosts {
			out.Groups[i].Hosts[j].Vars = redactInventoryVars(out.Groups[i].Hosts[j].Vars)
		}
	}
	writeJSON(w, http.StatusOK, out)
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
