package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/slim/ansiblehub/server/internal/auth"
	"github.com/slim/ansiblehub/server/internal/model"
	"github.com/slim/ansiblehub/server/internal/store"
)

type Server struct {
	store         store.Store
	log           *slog.Logger
	mux           *http.ServeMux
	opAuth        *OpAuth
	secureCookies bool
}

func New(st store.Store, log *slog.Logger, opAuth *OpAuth, secureCookies bool) *Server {
	s := &Server{store: st, log: log, mux: http.NewServeMux(), opAuth: opAuth, secureCookies: secureCookies}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

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
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "time": time.Now().UTC().Format(time.RFC3339)})
}

// ---- instances (operator) ----

func (s *Server) handleListInstances(w http.ResponseWriter, r *http.Request) {
	limit, offset := paginate(r, 20)
	total, err := s.store.CountInstances(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	insts, err := s.store.ListInstances(r.Context(), limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]InstanceWithApps, 0, len(insts))
	for _, inst := range insts {
		as, err := s.appsForInstance(r.Context(), inst.ID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, InstanceWithApps{Instance: inst, Apps: as})
	}
	writePage(w, out, total, limit, offset)
}

func (s *Server) appsForInstance(ctx context.Context, instanceID string) ([]AppStatus, error) {
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
			Application: app,
			AutoHeal:    effectiveAutoHeal(app, a),
			LastCheckAt: a.LastCheckAt,
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
		Name string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	token, err := auth.NewToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	inst, err := s.store.CreateInstance(r.Context(), model.Instance{
		ID:        genInstanceID(),
		Name:      body.Name,
		TokenHash: auth.Hash(token),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
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
	as, err := s.appsForInstance(r.Context(), inst.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, InstanceWithApps{Instance: inst, Apps: as})
}

func (s *Server) handleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	id, _ := idParam(r, "id")
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
	if err := s.store.Assign(r.Context(), id, body.AppID); err != nil {
		if errors.Is(err, store.ErrAlreadyBound) {
			writeErr(w, http.StatusConflict, "already bound")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
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
	if err := s.store.Unassign(r.Context(), id, appID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok200(w)
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
	c, err := s.store.CreateCommand(r.Context(), model.Command{
		InstanceID: id,
		AppID:      body.AppID,
		Action:     action,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	id, _ := idParam(r, "id")
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
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	runs, err := s.store.ListRuns(r.Context(), f)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writePage(w, runs, total, limit, offset)
}

// ---- applications (operator) ----

func (s *Server) handleListApplications(w http.ResponseWriter, r *http.Request) {
	apps, err := s.store.ListApplications(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
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
	app, err := s.store.CreateApplication(r.Context(), in)
	if err != nil {
		if errors.Is(err, store.ErrConflict) || strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, http.StatusConflict, "application name already exists")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
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
	var in model.Application
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	old, err := s.store.GetApplication(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "application not found")
		return
	}
	in.ID = old.ID
	if in.Branch == "" {
		in.Branch = old.Branch
	}
	if in.Path == "" {
		in.Path = old.Path
	}
	if in.Vars == "" {
		in.Vars = "{}"
	}
	if err := s.store.UpdateApplication(r.Context(), in); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
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
	repos, err := s.store.ListRepositories(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]repoView, 0, len(repos))
	for _, repo := range repos {
		out = append(out, toRepoView(repo))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateRepository(w http.ResponseWriter, r *http.Request) {
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
		if strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, http.StatusConflict, "repository url already exists")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toRepoView(repo))
}

func (s *Server) handleUpdateRepository(w http.ResponseWriter, r *http.Request) {
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
	// Empty password/ssh_key means "keep existing".
	if in.Password == "" {
		in.Password = old.Password
	}
	if in.SSHKey == "" {
		in.SSHKey = old.SSHKey
	}
	if err := s.store.UpdateRepository(r.Context(), in); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, http.StatusConflict, "repository url already exists")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toRepoView(in))
}

func (s *Server) handleDeleteRepository(w http.ResponseWriter, r *http.Request) {
	id, ok := repoIDParam(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "missing repo id")
		return
	}
	if err := s.store.DeleteRepository(r.Context(), id); err != nil {
		writeErr(w, http.StatusNotFound, "repository not found")
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
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := checkinResponse{}
	ass, err := s.store.ListAssignments(r.Context(), inst.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, a := range ass {
		app, err := s.store.GetApplication(r.Context(), a.AppID)
		if err != nil {
			continue
		}
		app.AutoHeal = effectiveAutoHeal(app, a)
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
		writeErr(w, http.StatusInternalServerError, err.Error())
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

	// The periodic/repo/after-apply checks poll constantly at fixed intervals,
	// so most of them repeat an identical state. Skip storing a new run when
	// nothing changed since the last recorded check — but always keep
	// operator-triggered ("command") runs for audit.
	if body.Mode == "check" && body.Trigger != "command" {
		if prev, err := s.store.LatestRun(r.Context(), inst.ID, body.AppID, "check"); err == nil && sameRunState(prev, run) {
			s.store.TouchCheck(r.Context(), inst.ID, body.AppID, run.FinishedAt)
			if err := s.refreshInstanceState(r.Context(), inst); err != nil {
				writeErr(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "deduped": true})
			return
		}
	}
	if err := s.store.TouchCheck(r.Context(), inst.ID, body.AppID, run.FinishedAt); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.store.CreateRun(r.Context(), run); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if body.Mode == "check" {
		if err := s.refreshInstanceState(r.Context(), inst); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
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
