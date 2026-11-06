package api

import (
	"context"
	"errors"
	"net/http"
	"path"
	"strings"

	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/store"
)

type configRepoInput struct {
	Name         *string `json:"name"`
	URL          *string `json:"url"`
	Branch       *string `json:"branch"`
	Path         *string `json:"path"`
	PollInterval *int    `json:"poll_interval"`
	Enabled      *bool   `json:"enabled"`
	Prune        *bool   `json:"prune"`
}

func (in configRepoInput) apply(repo *model.ConfigRepo) error {
	if in.Name != nil {
		repo.Name = strings.TrimSpace(*in.Name)
	}
	if in.URL != nil {
		repo.URL = strings.TrimSpace(*in.URL)
	}
	if in.Branch != nil {
		repo.Branch = strings.TrimSpace(*in.Branch)
	}
	if in.Path != nil {
		p, err := cleanConfigPath(*in.Path)
		if err != nil {
			return err
		}
		repo.Path = p
	}
	if in.PollInterval != nil {
		repo.PollInterval = *in.PollInterval
	}
	if in.Enabled != nil {
		repo.Enabled = *in.Enabled
	}
	if in.Prune != nil {
		repo.Prune = *in.Prune
	}
	if repo.Branch == "" {
		repo.Branch = "main"
	}
	if repo.PollInterval == 0 {
		repo.PollInterval = 300
	}
	if repo.PollInterval < 10 {
		return errors.New("poll_interval must be at least 10 seconds")
	}
	if repo.Name == "" || repo.URL == "" {
		return errors.New("name and url are required")
	}
	return nil
}

func cleanConfigPath(p string) (string, error) {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" || p == "." {
		return "", nil
	}
	if strings.HasPrefix(p, "/") {
		return "", errors.New("path must be relative to the repository root")
	}
	clean := path.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("path must stay inside the repository")
	}
	return clean, nil
}

func (s *Server) handleListConfigRepos(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "configrepos.get") {
		return
	}
	out, err := s.store.ListConfigRepos(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateConfigRepo(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "configrepos.create") {
		return
	}
	var in configRepoInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	repo := model.ConfigRepo{Enabled: true}
	if err := in.apply(&repo); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := s.store.CreateConfigRepo(r.Context(), repo)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeErr(w, http.StatusConflict, "config repo already exists")
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.triggerConfigRepo(out.ID, out.Enabled)
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleUpdateConfigRepo(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "configrepos.update") {
		return
	}
	id, ok := repoIDParam(r)
	if !ok || id == 0 {
		writeErr(w, http.StatusBadRequest, "invalid config repo id")
		return
	}
	repo, err := s.store.GetConfigRepo(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "config repo not found")
			return
		}
		s.internalError(w, r, err)
		return
	}
	var in configRepoInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if err := in.apply(&repo); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.UpdateConfigRepo(r.Context(), repo); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeErr(w, http.StatusConflict, "config repo name already exists")
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "config repo not found")
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.triggerConfigRepo(repo.ID, repo.Enabled)
	writeJSON(w, http.StatusOK, repo)
}

func (s *Server) handleDeleteConfigRepo(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "configrepos.delete") {
		return
	}
	id, ok := repoIDParam(r)
	if !ok || id == 0 {
		writeErr(w, http.StatusBadRequest, "invalid config repo id")
		return
	}
	if _, err := s.store.GetConfigRepo(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "config repo not found")
			return
		}
		s.internalError(w, r, err)
		return
	}
	if err := s.store.DeleteConfigRepo(r.Context(), id); err != nil {
		s.internalError(w, r, err)
		return
	}
	ok200(w)
}

func (s *Server) handleSyncConfigRepo(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "configrepos.sync") {
		return
	}
	id, ok := repoIDParam(r)
	if !ok || id == 0 {
		writeErr(w, http.StatusBadRequest, "invalid config repo id")
		return
	}
	if s.reconciler == nil {
		writeErr(w, http.StatusServiceUnavailable, "gitops reconciler is not configured")
		return
	}
	if err := s.reconciler.Sync(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "config repo not found")
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	repo, err := s.store.GetConfigRepo(r.Context(), id)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, repo)
}

// triggerConfigRepo starts a background reconcile after a create/update. It is
// best-effort: the polling loop retries anyway.
func (s *Server) triggerConfigRepo(id int64, enabled bool) {
	if s.reconciler == nil || !enabled {
		return
	}
	go func() {
		if err := s.reconciler.Sync(context.Background(), id); err != nil {
			s.log.Warn("gitops: initial sync failed", "config_repo_id", id, "error", err)
		}
	}()
}
