package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/slim/helios/server/internal/auth"
	"github.com/slim/helios/server/internal/envutil"
	"github.com/slim/helios/server/internal/metrics"
	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/notify"
	"github.com/slim/helios/server/internal/store"
	"github.com/slim/helios/server/internal/syncwindows"
)

var revisionRe = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

type Server struct {
	store         store.Store
	log           *slog.Logger
	mux           *http.ServeMux
	opAuth        *OpAuth
	secureCookies bool
	// maxHealStreak is the number of consecutive apply→after-apply cycles that
	// may fail to converge before an assignment is flagged stuck (autoheal
	// stops until the playbook converges again).
	maxHealStreak int
	notifier      *notify.Notifier
	downloadsDir  string
}

// SetNotifier enables webhook notifications (nil disables them).
func (s *Server) SetNotifier(n *notify.Notifier) { s.notifier = n }

// notifyEvent enriches the event with the application name and dispatches it.
func (s *Server) notifyEvent(ctx context.Context, ev notify.Event) {
	if s.notifier == nil {
		return
	}
	if ev.AppID != 0 && ev.AppName == "" {
		if app, err := s.store.GetApplication(ctx, ev.AppID); err == nil {
			ev.AppName = app.Name
		}
	}
	s.notifier.Send(ev)
}

func New(st store.Store, log *slog.Logger, opAuth *OpAuth, secureCookies bool) *Server {
	s := &Server{
		store:         st,
		log:           log,
		mux:           http.NewServeMux(),
		opAuth:        opAuth,
		secureCookies: secureCookies,
		maxHealStreak: 3,
	}
	if v := envutil.Get("HELIOS_HEAL_MAX_STREAK"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			s.maxHealStreak = n
		}
	}
	s.routes()
	return s
}

// maxRequestBody bounds every request body: agent checkins carry run logs and
// diffs, but nothing legitimate approaches this size.
const maxRequestBody = 8 << 20 // 8 MiB

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetSecurityHeaders(w, r, s.secureCookies)
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		}
		s.mux.ServeHTTP(w, r)
	})
}

func genInstanceID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "ins-" + hex.EncodeToString(b)
}

func idParam(r *http.Request, name string) (string, bool) {
	v := r.PathValue(name)
	return v, v != ""
}

func appIDParam(r *http.Request) (int64, bool) {
	v := r.PathValue("appId")
	n, err := strconv.ParseInt(v, 10, 64)
	return n, err == nil
}

func repoIDParam(r *http.Request) (int64, bool) {
	v := r.PathValue("repoId")
	n, err := strconv.ParseInt(v, 10, 64)
	return n, err == nil
}

func boolStr(v string) bool {
	return strings.EqualFold(v, "true")
}

func intQuery(r *http.Request, name string, def int) int {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// paginate reads and clamps limit/offset query params.
func paginate(r *http.Request, defLimit int) (limit, offset int) {
	limit = intQuery(r, "limit", defLimit)
	if limit < 1 {
		limit = defLimit
	}
	if limit > 500 {
		limit = 500
	}
	offset = intQuery(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func writePage(w http.ResponseWriter, items any, total, limit, offset int) {
	writeJSON(w, http.StatusOK, struct {
		Items  any `json:"items"`
		Total  int `json:"total"`
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	}{Items: items, Total: total, Limit: limit, Offset: offset})
}

// ---- health ----

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		s.log.Warn("health check failed", "error", err)
		writeErr(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "time": time.Now().UTC().Format(time.RFC3339)})
}

// ---- instances (operator) ----

func (s *Server) handleListInstances(w http.ResponseWriter, r *http.Request) {
	limit, offset := paginate(r, 20)
	all, scopes := s.visibleProjects(r, "instances", "get")
	var (
		total int
		insts []model.Instance
		err   error
	)
	if all {
		total, err = s.store.CountInstances(r.Context())
		if err == nil {
			insts, err = s.store.ListInstances(r.Context(), limit, offset)
		}
	} else {
		total, err = s.store.CountInstancesScoped(r.Context(), scopes)
		if err == nil {
			insts, err = s.store.ListInstancesScoped(r.Context(), scopes, limit, offset)
		}
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	windows, _ := s.store.ListSyncWindows(r.Context())
	out := make([]InstanceWithApps, 0, len(insts))
	for _, inst := range insts {
		as, err := s.appsForInstance(r.Context(), inst.ID, windows)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		out = append(out, InstanceWithApps{Instance: inst, Apps: as})
	}
	writePage(w, out, total, limit, offset)
}

func (s *Server) appsForInstance(ctx context.Context, instanceID string, windows []model.SyncWindow) ([]AppStatus, error) {
	ass, err := s.store.ListAssignments(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	out := make([]AppStatus, 0, len(ass))
	for _, a := range ass {
		app, err := s.store.GetApplication(ctx, a.AppID)
		if err != nil {
			continue
		}
		as := AppStatus{
			Application:    app,
			AutoHeal:       effectiveAutoHeal(app, a),
			Stuck:          a.Stuck,
			StuckAt:        a.StuckAt,
			LastCheckAt:    a.LastCheckAt,
			TargetRevision: a.TargetRevision,
			Frozen:         syncwindows.Frozen(windows, app.ID, time.Now()),
		}
		if a.AutoHeal != nil {
			as.AutoHealSet = true
		}
		check, err := s.store.LatestRun(ctx, instanceID, a.AppID, "check")
		if err == nil {
			as.LatestCheck = check
			as.HasCheck = true
		}
		apply, err := s.store.LatestRun(ctx, instanceID, a.AppID, "apply")
		if err == nil {
			as.LatestApply = apply
		}
		out = append(out, as)
	}
	return out, nil
}

// effectiveAutoHeal resolves an assignment-level override, falling back to the
// application default.
func effectiveAutoHeal(app model.Application, a model.Assignment) bool {
	if a.AutoHeal != nil {
		return *a.AutoHeal
	}
	return app.AutoHeal
}

// handleCreateInstance registers a new instance and returns its bootstrap token once.
func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string `json:"name"`
		Project string `json:"project"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	body.Project = strings.TrimSpace(body.Project)
	if !s.requireObject(w, r, "instances.create", body.Project) {
		return
	}
	if msg, err := s.validateProjectOnly(r.Context(), body.Project); err != nil {
		s.internalError(w, r, err)
		return
	} else if msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	token, err := auth.NewToken()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	inst, err := s.store.CreateInstance(r.Context(), model.Instance{
		ID:        genInstanceID(),
		Name:      body.Name,
		Project:   body.Project,
		TokenHash: auth.Hash(token),
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"instance": inst, "token": token})
}

func (s *Server) handleGetInstance(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "missing id")
		return
	}
	inst, err := s.store.GetInstance(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	if !s.requireObject(w, r, "instances.get", inst.Project) {
		return
	}
	windows, _ := s.store.ListSyncWindows(r.Context())
	as, err := s.appsForInstance(r.Context(), inst.ID, windows)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, InstanceWithApps{Instance: inst, Apps: as})
}

func (s *Server) handleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	id, _ := idParam(r, "id")
	inst, err := s.store.GetInstance(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	if !s.requireObject(w, r, "instances.delete", inst.Project) {
		return
	}
	if err := s.store.DeleteInstance(r.Context(), id); err != nil {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	ok200(w)
}

func (s *Server) handleAssign(w http.ResponseWriter, r *http.Request) {
	id, _ := idParam(r, "id")
	var body struct {
		AppID int64 `json:"app_id"`
	}
	if err := readJSON(r, &body); err != nil || body.AppID == 0 {
		writeErr(w, http.StatusBadRequest, "app_id is required")
		return
	}
	inst, err := s.store.GetInstance(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	app, err := s.store.GetApplication(r.Context(), body.AppID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireObject(w, r, "instances.update", inst.Project) {
		return
	}
	if !s.requireObject(w, r, "applications.get", app.Project) {
		return
	}
	if app.Project != inst.Project {
		writeErr(w, http.StatusConflict, "application and instance belong to different projects")
		return
	}
	if err := s.store.Assign(r.Context(), id, body.AppID); err != nil {
		if errors.Is(err, store.ErrAlreadyBound) {
			writeErr(w, http.StatusConflict, "already bound")
			return
		}
		s.internalError(w, r, err)
		return
	}
	ok200(w)
}

func (s *Server) handleUnassign(w http.ResponseWriter, r *http.Request) {
	id, _ := idParam(r, "id")
	appID, ok := appIDParam(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "missing app id")
		return
	}
	if !s.requireInstanceScope(w, r, id, "instances.update") {
		return
	}
	if err := s.store.Unassign(r.Context(), id, appID); err != nil {
		s.internalError(w, r, err)
		return
	}
	ok200(w)
}

// handleSetRevision pins or unpins an assignment to a git revision. Pinning a
// revision immediately queues an apply so the rollback happens on the next
// agent pass; unpinning resumes tracking the application branch.
func (s *Server) handleSetRevision(w http.ResponseWriter, r *http.Request) {
	id, _ := idParam(r, "id")
	appID, ok := appIDParam(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "missing app id")
		return
	}
	var body struct {
		Revision string `json:"revision"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	rev := strings.TrimSpace(body.Revision)
	if rev != "" && !revisionRe.MatchString(rev) {
		writeErr(w, http.StatusBadRequest, "revision must be a full or abbreviated git commit SHA")
		return
	}
	if !s.requireInstanceScope(w, r, id, "instances.update") {
		return
	}
	if _, err := s.store.GetAssignment(r.Context(), id, appID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusConflict, "instance is not bound to this application")
			return
		}
		s.internalError(w, r, err)
		return
	}
	if rev != "" && s.frozen(r.Context(), appID) {
		metrics.Frozen(appID)
		s.notifyEvent(r.Context(), notify.Event{
			Event: "sync_blocked", InstanceID: id, AppID: appID,
			Mode: model.ActionApply, Message: "rollback frozen by a maintenance window",
		})
		writeErr(w, http.StatusConflict, "sync is frozen by a maintenance window")
		return
	}
	if err := s.store.SetAssignmentTargetRevision(r.Context(), id, appID, rev); err != nil {
		s.internalError(w, r, err)
		return
	}
	if rev != "" {
		if _, err := s.store.CreateCommand(r.Context(), model.Command{
			InstanceID: id,
			AppID:      appID,
			Action:     model.ActionApply,
		}); err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"target_revision": rev})
}

func (s *Server) handleCreateInstanceCommand(w http.ResponseWriter, r *http.Request) {
	id, _ := idParam(r, "id")
	action := r.PathValue("action")
	var body struct {
		AppID int64 `json:"app_id"`
	}
	if err := readJSON(r, &body); err != nil || body.AppID == 0 {
		writeErr(w, http.StatusBadRequest, "app_id is required")
		return
	}
	if action != model.ActionCheck && action != model.ActionApply {
		writeErr(w, http.StatusBadRequest, "action must be check|apply")
		return
	}
	if !s.requireInstanceScope(w, r, id, "commands.create") {
		return
	}
	// Reject sync commands for an app the instance isn't bound to.
	ass, _ := s.store.ListAssignments(r.Context(), id)
	bound := false
	for _, a := range ass {
		if a.AppID == body.AppID {
			bound = true
			break
		}
	}
	if !bound {
		writeErr(w, http.StatusConflict, "instance is not bound to this application")
		return
	}
	if action == model.ActionApply && s.frozen(r.Context(), body.AppID) {
		metrics.Frozen(body.AppID)
		s.notifyEvent(r.Context(), notify.Event{
			Event: "sync_blocked", InstanceID: id, AppID: body.AppID,
			Mode: model.ActionApply, Message: "sync frozen by a maintenance window",
		})
		writeErr(w, http.StatusConflict, "sync is frozen by a maintenance window")
		return
	}
	c, err := s.store.CreateCommand(r.Context(), model.Command{
		InstanceID: id,
		AppID:      body.AppID,
		Action:     action,
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	id, _ := idParam(r, "id")
	if !s.requireInstanceScope(w, r, id, "runs.get") {
		return
	}
	limit, offset := paginate(r, 50)
	f := model.RunFilter{InstanceID: id, Limit: limit, Offset: offset}
	if v := r.URL.Query().Get("app_id"); v != "" {
		f.AppID, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := r.URL.Query().Get("mode"); v != "" {
		f.Mode = v
	}
	total, err := s.store.CountRuns(r.Context(), f)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	runs, err := s.store.ListRuns(r.Context(), f)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writePage(w, runs, total, limit, offset)
}

// handleListApplicationRuns returns the revision history of an application
// across every instance it is assigned to.
func (s *Server) handleListApplicationRuns(w http.ResponseWriter, r *http.Request) {
	id, ok := appIDParam(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "missing app id")
		return
	}
	app, err := s.store.GetApplication(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireObject(w, r, "runs.get", app.Project) {
		return
	}
	limit, offset := paginate(r, 50)
	f := model.RunFilter{AppID: id, Limit: limit, Offset: offset}
	if v := r.URL.Query().Get("mode"); v != "" {
		f.Mode = v
	}
	total, err := s.store.CountRuns(r.Context(), f)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	runs, err := s.store.ListRuns(r.Context(), f)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	// The history view only needs the revision metadata, not console output.
	for i := range runs {
		runs[i].OutputTail = ""
		runs[i].ChangedTasks = nil
		runs[i].Summary = nil
	}
	writePage(w, runs, total, limit, offset)
}

func (s *Server) frozen(ctx context.Context, appID int64) bool {
	windows, err := s.store.ListSyncWindows(ctx)
	if err != nil {
		// A window lookup failure must not block operations; the caller still
		// enforces every other safety gate.
		s.log.Warn("sync window lookup failed", "error", err)
		return false
	}
	return syncwindows.Frozen(windows, appID, time.Now())
}

// ---- audit log (operator) ----

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "audit.get") {
		return
	}
	limit, offset := paginate(r, 50)
	entries, total, err := s.store.ListAudit(r.Context(), limit, offset)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writePage(w, entries, total, limit, offset)
}

// ---- sync windows (operator) ----

func (s *Server) handleListSyncWindows(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "windows.get") {
		return
	}
	out, err := s.store.ListSyncWindows(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateSyncWindow(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "windows.create") {
		return
	}
	raw, err := readRawJSON(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	var in model.SyncWindow
	if err := decodeJSON(raw, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	var present map[string]json.RawMessage
	_ = json.Unmarshal(raw, &present)
	if _, ok := present["enabled"]; !ok {
		in.Enabled = true
	}
	in, msg := syncwindows.Validate(in)
	if msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if in.AppID != 0 {
		if _, err := s.store.GetApplication(r.Context(), in.AppID); err != nil {
			writeErr(w, http.StatusBadRequest, "unknown app_id")
			return
		}
	}
	out, err := s.store.CreateSyncWindow(r.Context(), in)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleUpdateSyncWindow(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "windows.update") {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("windowId"), 10, 64)
	if err != nil || id == 0 {
		writeErr(w, http.StatusBadRequest, "invalid window id")
		return
	}
	old, err := s.store.GetSyncWindow(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "window not found")
		return
	}
	raw, err := readRawJSON(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	var in model.SyncWindow
	if err := decodeJSON(raw, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	var present map[string]json.RawMessage
	if err := json.Unmarshal(raw, &present); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	in.ID = old.ID
	if _, ok := present["name"]; !ok || in.Name == "" {
		in.Name = old.Name
	}
	if _, ok := present["kind"]; !ok || in.Kind == "" {
		in.Kind = old.Kind
	}
	if _, ok := present["app_id"]; !ok {
		in.AppID = old.AppID
	}
	if _, ok := present["timezone"]; !ok || in.Timezone == "" {
		in.Timezone = old.Timezone
	}
	if _, ok := present["days"]; !ok {
		in.Days = old.Days
	}
	if _, ok := present["start_min"]; !ok {
		in.StartMin = old.StartMin
	}
	if _, ok := present["end_min"]; !ok {
		in.EndMin = old.EndMin
	}
	if _, ok := present["enabled"]; !ok {
		in.Enabled = old.Enabled
	}
	in, msg := syncwindows.Validate(in)
	if msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if in.AppID != 0 {
		if _, err := s.store.GetApplication(r.Context(), in.AppID); err != nil {
			writeErr(w, http.StatusBadRequest, "unknown app_id")
			return
		}
	}
	if err := s.store.UpdateSyncWindow(r.Context(), in); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, in)
}

func (s *Server) handleDeleteSyncWindow(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "windows.delete") {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("windowId"), 10, 64)
	if err != nil || id == 0 {
		writeErr(w, http.StatusBadRequest, "invalid window id")
		return
	}
	if err := s.store.DeleteSyncWindow(r.Context(), id); err != nil {
		s.internalError(w, r, err)
		return
	}
	ok200(w)
}

// ---- applications (operator) ----

func (s *Server) handleListApplications(w http.ResponseWriter, r *http.Request) {
	all, scopes := s.visibleProjects(r, "applications", "get")
	var (
		apps []model.Application
		err  error
	)
	if all {
		apps, err = s.store.ListApplications(r.Context())
	} else {
		apps, err = s.store.ListApplicationsScoped(r.Context(), scopes)
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, apps)
}

func (s *Server) handleCreateApplication(w http.ResponseWriter, r *http.Request) {
	var in model.Application
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if in.Name == "" || in.RepoURL == "" {
		writeErr(w, http.StatusBadRequest, "name and repo_url are required")
		return
	}
	if in.Branch == "" {
		in.Branch = "main"
	}
	if in.Path == "" {
		in.Path = "site.yml"
	}
	if in.Vars == "" {
		in.Vars = "{}"
	}
	if in.SyncInterval < 0 || (in.SyncInterval > 0 && in.SyncInterval < 10) {
		writeErr(w, http.StatusBadRequest, "sync_interval must be 0 (inherit) or at least 10 seconds")
		return
	}
	in.Project = strings.TrimSpace(in.Project)
	if !s.requireObject(w, r, "applications.create", in.Project) {
		return
	}
	if msg, err := s.validateAppProject(r.Context(), in.Project, in.RepoURL); err != nil {
		s.internalError(w, r, err)
		return
	} else if msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	// Ensure the repo is registered (ArgoCD-style: app → registry upsert).
	if _, err := s.store.EnsureRepositoryByURL(r.Context(), in.RepoURL); err != nil {
		s.internalError(w, r, err)
		return
	}
	app, err := s.store.CreateApplication(r.Context(), in)
	if err != nil {
		if errors.Is(err, store.ErrConflict) || strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, http.StatusConflict, "application name already exists")
			return
		}
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, app)
}

func (s *Server) handleUpdateApplication(w http.ResponseWriter, r *http.Request) {
	id, ok := appIDParam(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "missing app id")
		return
	}
	raw, err := readRawJSON(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	var in model.Application
	if err := decodeJSON(raw, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	var present map[string]json.RawMessage
	if err := json.Unmarshal(raw, &present); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	old, err := s.store.GetApplication(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireObject(w, r, "applications.update", old.Project) {
		return
	}
	if _, ok := present["project"]; !ok {
		in.Project = old.Project
	}
	in.Project = strings.TrimSpace(in.Project)
	if in.Project != old.Project {
		if !s.requireObject(w, r, "applications.update", in.Project) {
			return
		}
	}
	if msg, err := s.validateAppProject(r.Context(), in.Project, in.RepoURL); err != nil {
		s.internalError(w, r, err)
		return
	} else if msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	in.ID = old.ID
	if in.Name == "" {
		in.Name = old.Name
	}
	if in.RepoURL == "" {
		in.RepoURL = old.RepoURL
	}
	if in.Branch == "" {
		in.Branch = old.Branch
	}
	if in.Path == "" {
		in.Path = old.Path
	}
	// Omitted fields keep their stored value: a partial PUT must not silently
	// clear vars or flip autoheal to false.
	if _, ok := present["vars"]; !ok || in.Vars == "" {
		in.Vars = old.Vars
	}
	if _, ok := present["autoheal"]; !ok {
		in.AutoHeal = old.AutoHeal
	}
	if _, ok := present["sync_interval"]; !ok {
		in.SyncInterval = old.SyncInterval
	}
	if _, ok := present["ignore_paths"]; !ok {
		in.IgnorePaths = old.IgnorePaths
	}
	if in.SyncInterval < 0 || (in.SyncInterval > 0 && in.SyncInterval < 10) {
		writeErr(w, http.StatusBadRequest, "sync_interval must be 0 (inherit) or at least 10 seconds")
		return
	}
	if in.Vars == "" {
		in.Vars = "{}"
	}
	// Keep the registry in sync with the (possibly re-pointed) repo URL.
	if _, err := s.store.EnsureRepositoryByURL(r.Context(), in.RepoURL); err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.store.UpdateApplication(r.Context(), in); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeErr(w, http.StatusConflict, "application name already exists")
			return
		}
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, in)
}

func (s *Server) handleDeleteApplication(w http.ResponseWriter, r *http.Request) {
	id, ok := appIDParam(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "missing app id")
		return
	}
	app, err := s.store.GetApplication(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireObject(w, r, "applications.delete", app.Project) {
		return
	}
	if err := s.store.DeleteApplication(r.Context(), id); err != nil {
		writeErr(w, http.StatusNotFound, "application not found")
		return
	}
	ok200(w)
}

// ---- repositories (operator) ----

// repoView is the operator-facing shape of a repository: credentials are never
// returned, only flags telling the UI they exist.
type repoView struct {
	ID                 int64     `json:"id"`
	Name               string    `json:"name"`
	URL                string    `json:"url"`
	Username           string    `json:"username"`
	PasswordSet        bool      `json:"password_set"`
	SSHKeySet          bool      `json:"ssh_key_set"`
	InsecureSkipVerify bool      `json:"insecure_skip_verify"`
	CreatedAt          time.Time `json:"created_at"`
}

func toRepoView(r model.Repository) repoView {
	return repoView{
		ID:                 r.ID,
		Name:               r.Name,
		URL:                r.URL,
		Username:           r.Username,
		PasswordSet:        r.Password != "",
		SSHKeySet:          r.SSHKey != "",
		InsecureSkipVerify: r.InsecureSkipVerify,
		CreatedAt:          r.CreatedAt,
	}
}

func (s *Server) handleListRepositories(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "repositories.get") {
		return
	}
	repos, err := s.store.ListRepositories(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]repoView, 0, len(repos))
	for _, repo := range repos {
		out = append(out, toRepoView(repo))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateRepository(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "repositories.create") {
		return
	}
	var in model.Repository
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if in.URL == "" {
		writeErr(w, http.StatusBadRequest, "url is required")
		return
	}
	if in.Name == "" {
		in.Name = in.URL
	}
	repo, err := s.store.CreateRepository(r.Context(), in)
	if err != nil {
		if errors.Is(err, store.ErrConflict) || strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, http.StatusConflict, "repository url already exists")
			return
		}
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toRepoView(repo))
}

func (s *Server) handleUpdateRepository(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "repositories.update") {
		return
	}
	id, ok := repoIDParam(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "missing repo id")
		return
	}
	old, err := s.store.GetRepository(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "repository not found")
		return
	}
	var in model.Repository
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	in.ID = old.ID
	if in.URL == "" {
		in.URL = old.URL
	}
	if in.Name == "" {
		in.Name = in.URL
	}
	// Empty username/password/ssh_key means "keep existing".
	if in.Username == "" {
		in.Username = old.Username
	}
	if in.Password == "" {
		in.Password = old.Password
	}
	if in.SSHKey == "" {
		in.SSHKey = old.SSHKey
	}
	in.CreatedAt = old.CreatedAt
	if err := s.store.UpdateRepository(r.Context(), in); err != nil {
		if errors.Is(err, store.ErrConflict) || strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, http.StatusConflict, "repository url already exists")
			return
		}
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toRepoView(in))
}

func (s *Server) handleDeleteRepository(w http.ResponseWriter, r *http.Request) {
	if !s.requireGlobal(w, r, "repositories.delete") {
		return
	}
	id, ok := repoIDParam(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "missing repo id")
		return
	}
	repo, err := s.store.GetRepository(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "repository not found")
			return
		}
		s.internalError(w, r, err)
		return
	}
	apps, err := s.store.CountApplicationsByRepoURL(r.Context(), repo.URL)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if apps > 0 {
		writeErr(w, http.StatusConflict, fmt.Sprintf(
			"repository %q is still used by %d application(s); change or delete them first",
			repo.URL, apps))
		return
	}
	if err := s.store.DeleteRepository(r.Context(), id); err != nil {
		s.internalError(w, r, err)
		return
	}
	ok200(w)
}

// ---- agent endpoints (bearer auth) ----

// checkinPayload is what the agent sends on every poll.
type checkinPayload struct {
	InstanceID string `json:"instance_id"`
	Version    string `json:"version"`
	Hostname   string `json:"hostname"`
	Platform   string `json:"platform"`
}

type checkinResponse struct {
	Applications []model.Application `json:"applications"`
	Commands     []model.Command     `json:"commands"`
}

func (s *Server) handleCheckin(w http.ResponseWriter, r *http.Request) {
	var body checkinPayload
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	inst := r.Context().Value(ctxInstance).(model.Instance)
	if body.InstanceID != "" && body.InstanceID != inst.ID {
		// Agent re-registered under a different helper ID: keep canonical server ID.
		s.log.Warn("agent instance_id mismatch, using canonical", "reported", body.InstanceID, "canonical", inst.ID)
	}
	// Heartbeat: update liveness metadata.
	inst.Version = body.Version
	inst.Hostname = body.Hostname
	inst.Platform = body.Platform
	inst.LastSeen = time.Now().UTC()
	if err := s.store.UpdateInstance(r.Context(), inst); err != nil {
		s.internalError(w, r, err)
		return
	}

	windows, _ := s.store.ListSyncWindows(r.Context())
	metrics.Checkin(inst.ID)
	resp := checkinResponse{}
	ass, err := s.store.ListAssignments(r.Context(), inst.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	for _, a := range ass {
		app, err := s.store.GetApplication(r.Context(), a.AppID)
		if err != nil {
			continue
		}
		app.AutoHeal = effectiveAutoHeal(app, a)
		// Rollback/pinned revision wins over the application branch. The agent
		// stays unaware: it just sees a different target revision.
		if a.TargetRevision != "" {
			app.Branch = a.TargetRevision
		}
		// A non-converging assignment is stuck: keep drifting checks but stop
		// the apply side, so the box isn't pinned in an apply loop forever.
		if a.Stuck {
			app.AutoHeal = false
		}
		// Maintenance windows suppress autoheal but never checks.
		if syncwindows.Frozen(windows, app.ID, time.Now()) {
			app.AutoHeal = false
		}
		// Resolve the git connection from the repositories registry so the
		// agent can authenticate against private repos.
		if repo, err := s.store.GetRepositoryByURL(r.Context(), app.RepoURL); err == nil {
			app.Connection = &repo
		}
		// Merge: legacy vars < app defaults < this host's overrides.
		if merged, err := s.mergedVars(r.Context(), inst.ID, a.AppID, app.Vars); err == nil {
			app.Vars = merged
		}
		resp.Applications = append(resp.Applications, app)
	}
	// Claim pending commands atomically and hand them to this agent.
	resp.Commands, err = s.store.ClaimCommands(r.Context(), inst.ID)
	if err != nil && !errors.Is(err, store.ErrNoCommand) {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// statusPayload is the outcome of a check/apply run performed by the agent.
type statusPayload struct {
	AppID        int64               `json:"app_id"`
	Mode         string              `json:"mode"`
	Status       string              `json:"status"`
	Trigger      string              `json:"trigger,omitempty"`
	CommitBefore string              `json:"commit_before,omitempty"`
	CommitAfter  string              `json:"commit_after,omitempty"`
	CommitMsg    string              `json:"commit_msg,omitempty"`
	Summary      map[string]int      `json:"summary,omitempty"`
	ChangedTasks []model.ChangedTask `json:"changed_tasks,omitempty"`
	OutputTail   string              `json:"output_tail,omitempty"`
	Output       string              `json:"output,omitempty"`
	OutputText   string              `json:"output_text,omitempty"`
	OutputDiff   string              `json:"diff,omitempty"`
	Resources    []model.Resource    `json:"resources,omitempty"`
	StartedAt    time.Time           `json:"started_at"`
	FinishedAt   time.Time           `json:"finished_at"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	var body statusPayload
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	inst := r.Context().Value(ctxInstance).(model.Instance)
	if body.Mode != "check" && body.Mode != "apply" {
		writeErr(w, http.StatusBadRequest, "mode must be check|apply")
		return
	}
	switch body.Status {
	case model.RunOK, model.RunDrift, model.RunFailed:
	default:
		writeErr(w, http.StatusBadRequest, "status must be ok|drift|failed")
		return
	}
	if _, err := s.store.GetAssignment(r.Context(), inst.ID, body.AppID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusConflict, "instance is not bound to this application")
			return
		}
		s.internalError(w, r, err)
		return
	}
	metrics.Run(inst.ID, body.AppID, body.Mode, body.Status)
	run := model.Run{
		InstanceID:   inst.ID,
		AppID:        body.AppID,
		Mode:         body.Mode,
		Status:       body.Status,
		Trigger:      body.Trigger,
		CommitBefore: body.CommitBefore,
		CommitAfter:  body.CommitAfter,
		CommitMsg:    body.CommitMsg,
		Summary:      body.Summary,
		ChangedTasks: body.ChangedTasks,
		OutputTail:   body.OutputTail,
		Output:       body.Output,
		OutputText:   body.OutputText,
		OutputDiff:   body.OutputDiff,
		Resources:    body.Resources,
		StartedAt:    body.StartedAt,
		FinishedAt:   body.FinishedAt,
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}
	if run.FinishedAt.IsZero() {
		run.FinishedAt = time.Now().UTC()
	}

	// Healing guard: an after-apply check that still drifts means the apply
	// did not converge. After maxHealStreak consecutive failures, flag the
	// assignment stuck so the agent stops applying (drift checks continue).
	// Any converged check resets the counter and lifts the flag.
	if body.Mode == "check" {
		if body.Trigger == "after_apply" && body.Status == model.RunDrift {
			if app, err := s.store.GetApplication(r.Context(), body.AppID); err == nil {
				if a, err := s.store.GetAssignment(r.Context(), inst.ID, body.AppID); err == nil && effectiveAutoHeal(app, a) {
					streak, _ := s.store.BumpDriftStreak(r.Context(), inst.ID, body.AppID)
					if streak >= s.maxHealStreak {
						if err := s.store.SetStuck(r.Context(), inst.ID, body.AppID, true); err != nil {
							s.log.Warn("failed to flag assignment stuck", "instance", inst.ID, "app", body.AppID, "error", err)
						} else {
							s.log.Warn("assignment stuck: autoheal did not converge",
								"instance", inst.ID, "app", body.AppID, "streak", streak)
							s.notifyEvent(r.Context(), notify.Event{
								Event: "assignment_stuck", InstanceID: inst.ID, AppID: body.AppID,
								Status: model.RunDrift, Message: fmt.Sprintf("autoheal did not converge after %d cycles", streak),
							})
						}
					}
				}
			}
		} else if body.Status == model.RunOK {
			if err := s.store.ResetHealState(r.Context(), inst.ID, body.AppID); err != nil {
				s.log.Warn("failed to reset heal state", "instance", inst.ID, "app", body.AppID, "error", err)
			}
		}
	}

	// The periodic/repo/after-apply checks poll constantly at fixed intervals,
	// so most of them repeat an identical state. Skip storing a new run when
	// nothing changed since the last recorded check — but always keep
	// operator-triggered ("command") runs for audit.
	if body.Mode == "check" && body.Trigger != "command" {
		if prev, err := s.store.LatestRun(r.Context(), inst.ID, body.AppID, "check"); err == nil && sameRunState(prev, run) {
			s.store.TouchCheck(r.Context(), inst.ID, body.AppID, run.FinishedAt)
			if err := s.refreshInstanceState(r.Context(), inst); err != nil {
				s.internalError(w, r, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "deduped": true})
			return
		}
	}
	// Idempotent autoheal applies repeat the same state each non-converging
	// cycle; dedupe them the same way so a stuck app can't grow the DB forever.
	if body.Mode == "apply" && body.Trigger != "command" {
		if prev, err := s.store.LatestRun(r.Context(), inst.ID, body.AppID, "apply"); err == nil && sameRunState(prev, run) {
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "deduped": true})
			return
		}
	}
	if err := s.store.TouchCheck(r.Context(), inst.ID, body.AppID, run.FinishedAt); err != nil {
		s.internalError(w, r, err)
		return
	}
	if _, err := s.store.CreateRun(r.Context(), run); err != nil {
		s.internalError(w, r, err)
		return
	}
	switch {
	case body.Status == model.RunFailed:
		s.notifyEvent(r.Context(), notify.Event{
			Event: "run_failed", InstanceID: inst.ID, AppID: body.AppID,
			Mode: body.Mode, Status: body.Status, Trigger: body.Trigger,
			Commit: body.CommitAfter, Message: body.OutputTail,
		})
	case body.Mode == model.ActionApply && body.Status == model.RunOK &&
		(body.Trigger == "autoheal" || body.Trigger == "command"):
		s.notifyEvent(r.Context(), notify.Event{
			Event: "apply_ok", InstanceID: inst.ID, AppID: body.AppID,
			Mode: body.Mode, Status: body.Status, Trigger: body.Trigger,
			Commit: body.CommitAfter,
		})
	}
	if body.Mode == "check" {
		if err := s.refreshInstanceState(r.Context(), inst); err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// sameRunState reports whether two check runs describe the same desired state
// (drift-wise), so identical auto-checks can be deduplicated.
func sameRunState(a, b model.Run) bool {
	if a.Status != b.Status || a.CommitBefore != b.CommitBefore || a.CommitAfter != b.CommitAfter {
		return false
	}
	if len(a.Summary) != len(b.Summary) || len(a.ChangedTasks) != len(b.ChangedTasks) {
		return false
	}
	for k, v := range a.Summary {
		if b.Summary[k] != v {
			return false
		}
	}
	for i := range a.ChangedTasks {
		if a.ChangedTasks[i] != b.ChangedTasks[i] {
			return false
		}
	}
	return true
}

// refreshInstanceState recomputes the per-instance aggregated drift state from
// the latest check run of each bound application (drift > error > ok > unknown).
func (s *Server) refreshInstanceState(ctx context.Context, inst model.Instance) error {
	ass, err := s.store.ListAssignments(ctx, inst.ID)
	if err != nil {
		return err
	}
	state := model.StateUnknown
	if len(ass) == 0 {
		state = model.StateUnknown
	}
	for _, a := range ass {
		latest, err := s.store.LatestRun(ctx, inst.ID, a.AppID, "check")
		if err != nil {
			continue
		}
		inst.LastCommit = latest.CommitAfter
		switch latest.Status {
		case model.RunDrift:
			if state != model.StateDrift && state != model.StateError {
				state = model.StateDrift
			}
		case model.RunFailed:
			state = model.StateError
		case model.RunOK:
			if state == model.StateUnknown || state == "" {
				state = model.StateOK
			}
		}
	}
	if len(ass) == 0 {
		state = model.StateUnknown
	}
	inst.State = state
	inst.LastSeen = time.Now().UTC()
	return s.store.UpdateInstance(ctx, inst)
}
