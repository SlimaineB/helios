package api

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/slim/helios/server/internal/auth"
	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/store"
)

const (
	// runnerOnlineWindow is how long a runner may stay silent before the UI
	// marks it offline. Runners heartbeat every few seconds.
	runnerOnlineWindow = 45 * time.Second
	// runnerJobStaleAfter requeues a job claimed by a runner that disappeared.
	// It is longer than the Ansible timeout so a slow play is not run twice.
	runnerJobStaleAfter = 30 * time.Minute
)

// runnerAuth authenticates the runner with its shared bearer token.
// An empty server-side token disables the runner API entirely.
func (s *Server) runnerAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.runnerTokenHash == "" {
			writeErr(w, http.StatusServiceUnavailable, "runner API disabled")
			return
		}
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			writeErr(w, http.StatusUnauthorized, "missing runner token")
			return
		}
		got := auth.Hash(strings.TrimPrefix(header, "Bearer "))
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.runnerTokenHash)) != 1 {
			writeErr(w, http.StatusUnauthorized, "invalid runner token")
			return
		}
		next(w, r)
	}
}

func validRunnerID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c == '-' || c == '_' || c == '.' || c == ':' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

type runnerHeartbeat struct {
	RunnerID string   `json:"runner_id"`
	Tags     []string `json:"tags"`
	Version  string   `json:"version,omitempty"`
}

func (s *Server) handleRunnerHeartbeat(w http.ResponseWriter, r *http.Request) {
	var body runnerHeartbeat
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	body.RunnerID = strings.TrimSpace(body.RunnerID)
	if !validRunnerID(body.RunnerID) {
		writeErr(w, http.StatusBadRequest, "missing or invalid runner_id")
		return
	}
	if err := s.store.UpsertRunner(r.Context(), body.RunnerID, normalizeInstanceTags(body.Tags), body.Version, time.Now().UTC()); err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type runnerJobClaim struct {
	RunnerID string   `json:"runner_id"`
	Tags     []string `json:"tags"`
}

type runnerJobPayload struct {
	Job         model.RunnerJob   `json:"job"`
	Application model.Application `json:"application"`
}

// handleRunnerClaim claims the oldest queued job whose required tags are all
// present on the runner. A 204 means there is nothing to do right now.
func (s *Server) handleRunnerClaim(w http.ResponseWriter, r *http.Request) {
	var body runnerJobClaim
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	body.RunnerID = strings.TrimSpace(body.RunnerID)
	if !validRunnerID(body.RunnerID) {
		writeErr(w, http.StatusBadRequest, "missing or invalid runner_id")
		return
	}
	job, err := s.store.ClaimRunnerJob(r.Context(), body.RunnerID, normalizeInstanceTags(body.Tags), time.Now().UTC().Add(-runnerJobStaleAfter))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		s.internalError(w, r, err)
		return
	}
	app, err := s.store.GetApplication(r.Context(), job.AppID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if repo, err := s.store.GetRepositoryByProjectURL(r.Context(), app.Project, app.RepoURL); err == nil {
		app.Connection = &repo
	} else if !errors.Is(err, store.ErrNotFound) {
		s.log.Warn("runner job repository lookup failed", "app", app.Name, "error", err)
	}
	if vars, err := s.store.AppVariables(r.Context(), app.ID); err == nil {
		if merged, err := mergeVarsJSON(app.Vars, vars); err == nil {
			app.Vars = merged
		}
	}
	writeJSON(w, http.StatusOK, runnerJobPayload{Job: job, Application: app})
}

type runnerJobResult struct {
	RunnerID     string              `json:"runner_id"`
	Status       string              `json:"status"`
	Error        string              `json:"error,omitempty"`
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

func (s *Server) handleRunnerJobResult(w http.ResponseWriter, r *http.Request) {
	jobID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || jobID <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid job id")
		return
	}
	var body runnerJobResult
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	body.RunnerID = strings.TrimSpace(body.RunnerID)
	if !validRunnerID(body.RunnerID) {
		writeErr(w, http.StatusBadRequest, "missing or invalid runner_id")
		return
	}
	switch body.Status {
	case model.JobOK, model.JobDrift, model.JobFailed:
	default:
		writeErr(w, http.StatusBadRequest, "status must be ok, drift or failed")
		return
	}
	run, err := s.store.FinishRunnerJob(r.Context(), jobID, body.RunnerID, body.Status, body.Error, model.Run{
		Status:       body.Status,
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
	}, time.Now().UTC())
	if err != nil {
		if errors.Is(err, store.ErrLeaseLost) {
			writeErr(w, http.StatusConflict, "job is no longer owned by this runner")
			return
		}
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

type runnerFleetSummary struct {
	Runners int `json:"runners"`
	Online  int `json:"online"`
	Queued  int `json:"queued"`
	Running int `json:"running"`
}

type runnerFleet struct {
	Enabled bool               `json:"enabled"`
	Summary runnerFleetSummary `json:"summary"`
	Runners []model.Runner     `json:"runners"`
	Jobs    []model.RunnerJob  `json:"jobs"`
}

// handleListRunners is the operator view of the bastion fleet and recent jobs.
func (s *Server) handleListRunners(w http.ResponseWriter, r *http.Request) {
	runners, err := s.store.ListRunners(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	now := time.Now()
	out := runnerFleet{Enabled: s.runnerTokenHash != "", Runners: make([]model.Runner, 0, len(runners))}
	for _, runner := range runners {
		runner.Online = !runner.LastSeen.IsZero() && now.Sub(runner.LastSeen) <= runnerOnlineWindow
		out.Runners = append(out.Runners, runner)
		out.Summary.Runners++
		if runner.Online {
			out.Summary.Online++
		}
	}
	jobs, err := s.store.ListRunnerJobs(r.Context(), 0, 50)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out.Jobs = make([]model.RunnerJob, 0, len(jobs))
	for _, job := range jobs {
		if !s.allow(r, "runs.get", job.Project) {
			continue
		}
		out.Jobs = append(out.Jobs, job)
		switch job.Status {
		case model.JobQueued:
			out.Summary.Queued++
		case model.JobRunning:
			out.Summary.Running++
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleRunApplication queues a runner job for a runner-mode application.
func (s *Server) handleRunApplication(w http.ResponseWriter, r *http.Request) {
	id, ok := appIDParam(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "missing app id")
		return
	}
	var body struct {
		Mode string `json:"mode"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.Mode != model.ActionCheck && body.Mode != model.ActionApply {
		writeErr(w, http.StatusBadRequest, "mode must be check or apply")
		return
	}
	app, err := s.store.GetApplication(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "application not found")
		return
	}
	if !s.requireObject(w, r, "commands.create", app.Project) {
		return
	}
	if app.ExecutionMode != model.ExecRunner {
		writeErr(w, http.StatusConflict, "application is in agent mode; use instance commands")
		return
	}
	if body.Mode == model.ActionApply && s.frozen(r.Context(), app.ID) {
		writeErr(w, http.StatusConflict, "sync is locked globally or by a maintenance window")
		return
	}
	job, err := s.store.CreateRunnerJob(r.Context(), app.ID, body.Mode, "manual", app.RunnerTags)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}
