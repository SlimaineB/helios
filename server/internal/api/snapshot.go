package api

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/slim/helios/server/internal/gitops"
	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/repocache"
)

// SetRepoCache enables the snapshot endpoint. A nil cache disables it and
// agents continue to sync directly from git.
func (s *Server) SetRepoCache(c *repocache.Cache) { s.repoCache = c }

// handleAgentSnapshot resolves the assigned application's revision through the
// replica-local cache and streams a tarball (or 304 when the agent is current).
func (s *Server) handleAgentSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.repoCache == nil {
		writeErr(w, http.StatusServiceUnavailable, "repo cache disabled")
		return
	}
	// A cold mirror can spend minutes in git fetch/archive before the first
	// byte is written. Extend the response deadline before that work starts.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(10 * time.Minute))
	inst, ok := r.Context().Value(ctxInstance).(model.Instance)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "missing instance context")
		return
	}
	appID, err := strconv.ParseInt(r.PathValue("appId"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid application id")
		return
	}
	if _, err := s.store.GetAssignment(r.Context(), inst.ID, appID); err != nil {
		writeErr(w, http.StatusForbidden, "application is not assigned to this instance")
		return
	}
	app, err := s.store.GetApplication(r.Context(), appID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "application not found")
		return
	}
	creds := gitops.Creds{}
	if repo, err := s.store.GetRepositoryByURL(r.Context(), app.RepoURL); err == nil {
		creds = gitops.Creds{
			Username:           repo.Username,
			Password:           repo.Password,
			SSHKey:             repo.SSHKey,
			InsecureSkipVerify: repo.InsecureSkipVerify,
		}
	}

	current := strings.TrimPrefix(strings.TrimSpace(r.Header.Get("If-None-Match")), "W/")
	current = strings.Trim(current, `"`)

	load := func(currentCommit string) (repocache.Snapshot, bool) {
		snap, err := s.repoCache.Ensure(r.Context(), app.RepoURL, app.Branch, creds, currentCommit)
		if err != nil {
			if errors.Is(err, repocache.ErrUnsupportedRevision) {
				w.Header().Set("X-Helios-Fallback", "git")
				writeErr(w, http.StatusUnprocessableEntity, "revision is not cacheable")
				return repocache.Snapshot{}, false
			}
			s.internalError(w, r, err)
			return repocache.Snapshot{}, false
		}
		return snap, true
	}

	snap, ok := load(current)
	if !ok {
		return
	}
	setSnapshotHeaders(w, snap)
	if snap.NotModified {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	// A large tarball on a slow link can exceed the server default write
	// deadline. Extend it only for this response.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(10 * time.Minute))
	f, err := os.Open(snap.Path)
	if os.IsNotExist(err) {
		// The hourly prune may have removed the tarball between Ensure and
		// open. Regenerate once with an empty current commit, then retry.
		snap, ok = load("")
		if !ok {
			return
		}
		setSnapshotHeaders(w, snap)
		f, err = os.Open(snap.Path)
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, filepath.Base(snap.Path), info.ModTime(), f)
}

func setSnapshotHeaders(w http.ResponseWriter, snap repocache.Snapshot) {
	w.Header().Set("ETag", `"`+snap.Commit+`"`)
	w.Header().Set("X-Helios-Commit", snap.Commit)
	if snap.Subject != "" {
		w.Header().Set("X-Helios-Subject", url.QueryEscape(snap.Subject))
	} else {
		w.Header().Del("X-Helios-Subject")
	}
}
