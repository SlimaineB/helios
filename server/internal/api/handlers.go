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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/slim/helios/server/internal/auth"
	"github.com/slim/helios/server/internal/envutil"
	"github.com/slim/helios/server/internal/gitops"
	"github.com/slim/helios/server/internal/metrics"
	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/notify"
	"github.com/slim/helios/server/internal/ratelimit"
	"github.com/slim/helios/server/internal/rbac"
	"github.com/slim/helios/server/internal/reconciler"
	"github.com/slim/helios/server/internal/repocache"
	"github.com/slim/helios/server/internal/retention"
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
	// rbac is the live policy set. It starts as the file-loaded OpAuth.RBAC and
	// is reloaded from the database whenever rbac_policies/rbac_bindings change.
	rbacMu sync.RWMutex
	rbac   *rbac.RBAC
	// maxHealStreak is the number of consecutive apply→after-apply cycles that
	// may fail to converge before an assignment is flagged stuck (autoheal
	// stops until the playbook converges again).
	maxHealStreak int
	notifier      *notify.Notifier
	downloadsDir  string
	reconciler    *reconciler.Reconciler
	// Agentless tunnel mode: runners authenticate with a shared bearer token
	// and discover loopback ports allocated to tunnel instances.
	runnerTokenHash string
	tunnelGateway   string
	tunnelPortMin   int
	tunnelPortMax   int
	repoCache       *repocache.Cache
	settings        ServerSettings
	retention       *retention.Manager
	events          *eventHub
	authLimiter     *ratelimit.Limiter
	// trustProxy makes the server derive client IPs from X-Forwarded-For /
	// X-Real-IP. Only enable it when a trusted reverse proxy always overwrites
	// those headers, otherwise they can be spoofed to dodge rate limits.
	trustProxy bool
	bgCtx      context.Context
	bgCancel   context.CancelFunc
	bgMu       sync.Mutex
	bgClosed   bool
	bgWG       sync.WaitGroup
}

// SetNotifier enables webhook notifications (nil disables them).
func (s *Server) SetNotifier(n *notify.Notifier) { s.notifier = n }

// SetSettings stores the non-secret runtime configuration shown in the UI.
func (s *Server) SetSettings(v ServerSettings) { s.settings = v }

// SetRetention exposes the retention manager status in the Settings API.
func (s *Server) SetRetention(m *retention.Manager) { s.retention = m }

// SetReconciler enables the optional GitOps controller endpoints.
func (s *Server) SetReconciler(r *reconciler.Reconciler) { s.reconciler = r }

// SetRunnerToken enables the agentless runner API. An empty token disables it.
func (s *Server) SetRunnerToken(token string) {
	if token == "" {
		s.runnerTokenHash = ""
		return
	}
	s.runnerTokenHash = auth.Hash(token)
}

// SetTunnelConfig configures the reverse-SSH tunnel bootstrap advertised to
// operators and the loopback port range allocated to tunnel instances.
func (s *Server) SetTunnelConfig(gateway string, minPort, maxPort int) {
	s.tunnelGateway = strings.TrimSpace(gateway)
	if minPort > 0 {
		s.tunnelPortMin = minPort
	}
	if maxPort >= s.tunnelPortMin {
		s.tunnelPortMax = maxPort
	}
}

// Close cancels background GitOps work and waits for it to release leases
// before the caller closes the database.
func (s *Server) Close() {
	s.bgMu.Lock()
	s.bgClosed = true
	s.bgCancel()
	s.bgMu.Unlock()
	s.bgWG.Wait()
}

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
	bgCtx, bgCancel := context.WithCancel(context.Background())
	s := &Server{
		store:         st,
		log:           log,
		mux:           http.NewServeMux(),
		opAuth:        opAuth,
		secureCookies: secureCookies,
		maxHealStreak: 3,
		tunnelPortMin: 22000,
		tunnelPortMax: 22999,
		bgCtx:         bgCtx,
		bgCancel:      bgCancel,
		events:        newEventHub(),
	}
	if opAuth != nil {
		s.rbac = opAuth.RBAC
	}
	if v := envutil.Get("HELIOS_HEAL_MAX_STREAK"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			s.maxHealStreak = n
		}
	}
	s.authLimiter = ratelimit.New(authRateLimit(), authRateWindow())
	v := envutil.Get("HELIOS_TRUST_PROXY")
	s.trustProxy = boolStr(v) || v == "1"
	s.routes()
	return s
}

// authRateLimit returns the failed-auth attempts allowed per window and per
// client IP. Zero disables throttling.
func authRateLimit() int {
	v := envutil.Get("HELIOS_AUTH_RATE_LIMIT")
	if v == "" {
		return 10
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 10
	}
	return n
}

func authRateWindow() time.Duration {
	v := envutil.Get("HELIOS_AUTH_RATE_WINDOW")
	if v == "" {
		return time.Minute
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return time.Minute
	}
	return d
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
	f := model.InstanceListFilter{
		AllProjects:    all,
		Projects:       scopes,
		Query:          strings.TrimSpace(r.URL.Query().Get("q")),
		Tag:            strings.TrimSpace(r.URL.Query().Get("tag")),
		Mode:           strings.TrimSpace(r.URL.Query().Get("mode")),
		State:          strings.TrimSpace(r.URL.Query().Get("state")),
		OnlineOnly:     r.URL.Query().Get("online") == "1" || r.URL.Query().Get("online") == "true",
		RepoURL:        strings.TrimSpace(r.URL.Query().Get("repo")),
		UnassignedOnly: r.URL.Query().Get("unassigned") == "1",
		Limit:          limit,
		Offset:         offset,
		Now:            time.Now().UTC(),
	}
	if v := r.URL.Query().Get("project"); v != "" {
		f.HasProject = true
		if v != "__global__" {
			f.Project = v
		}
	}
	f.AppID, _ = strconv.ParseInt(r.URL.Query().Get("app_id"), 10, 64)
	f.NotAppID, _ = strconv.ParseInt(r.URL.Query().Get("not_app_id"), 10, 64)
	for _, raw := range strings.Split(r.URL.Query().Get("app_ids"), ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err == nil && id > 0 {
			f.AppIDs = append(f.AppIDs, id)
		}
	}
	total, err := s.store.CountInstancesFiltered(r.Context(), f)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	insts, err := s.store.ListInstancesFiltered(r.Context(), f)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	compact := r.URL.Query().Get("compact") == "1"
	apps := map[string][]AppStatus{}
	if !compact {
		windows, _ := s.store.ListSyncWindows(r.Context())
		apps, err = s.appsForInstances(r.Context(), insts, windows)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	appFilter := f.AppID
	if appFilter == 0 && len(f.AppIDs) > 0 {
		appFilter = f.AppIDs[0]
	}
	out := make([]InstanceWithApps, 0, len(insts))
	for _, inst := range insts {
		inst = s.decorateInstance(inst)
		as := apps[inst.ID]
		if as == nil {
			as = []AppStatus{}
		}
		if appFilter > 0 {
			filtered := as[:0]
			for _, status := range as {
				if status.Application.ID == appFilter || containsInt64(f.AppIDs, status.Application.ID) {
					filtered = append(filtered, status)
				}
			}
			as = filtered
		}
		out = append(out, InstanceWithApps{Instance: inst, Apps: as})
	}
	facets := model.InstanceFacets{Projects: []string{}, Tags: []string{}, Apps: []model.AppRef{}}
	if r.URL.Query().Get("facets") == "1" {
		facets, err = s.store.InstanceFacets(r.Context(), all, scopes)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	stats := model.InstanceListStats{}
	if r.URL.Query().Get("stats") == "1" {
		stats, err = s.store.InstanceListStats(r.Context(), f)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, struct {
		Items  []InstanceWithApps      `json:"items"`
		Total  int                     `json:"total"`
		Limit  int                     `json:"limit"`
		Offset int                     `json:"offset"`
		Facets model.InstanceFacets    `json:"facets"`
		Stats  model.InstanceListStats `json:"stats"`
	}{Items: out, Total: total, Limit: limit, Offset: offset, Facets: facets, Stats: stats})
}

func containsInt64(ids []int64, id int64) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

// DriftRow is one drifting or stuck assignment hydrated for the drift page.
type DriftRow struct {
	Instance model.Instance `json:"instance"`
	App      AppStatus      `json:"app"`
}

func (s *Server) handleApplicationStats(w http.ResponseWriter, r *http.Request) {
	all, scopes := s.visibleProjects(r, "applications", "get")
	stats, err := s.store.ApplicationAssignmentStats(r.Context(), all, scopes)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Items []model.ApplicationStats `json:"items"`
	}{Items: stats})
}

func (s *Server) handleListDrift(w http.ResponseWriter, r *http.Request) {
	limit, offset := paginate(r, 50)
	all, scopes := s.visibleProjects(r, "instances", "get")
	f := model.AssignmentFilter{
		AllProjects: all,
		Projects:    scopes,
		Query:       strings.TrimSpace(r.URL.Query().Get("q")),
		AppID:       0,
		Limit:       limit,
		Offset:      offset,
	}
	if v := r.URL.Query().Get("project"); v != "" {
		f.HasProject = true
		if v != "__global__" {
			f.Project = v
		}
	}
	f.AppID, _ = strconv.ParseInt(r.URL.Query().Get("app_id"), 10, 64)
	total, err := s.store.CountDriftAssignments(r.Context(), f)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	keys, err := s.store.ListDriftAssignments(r.Context(), f)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	stats, err := s.store.DriftAssignmentStats(r.Context(), f)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	instanceIDs := make([]string, 0, len(keys))
	seen := map[string]bool{}
	for _, key := range keys {
		if !seen[key.InstanceID] {
			seen[key.InstanceID] = true
			instanceIDs = append(instanceIDs, key.InstanceID)
		}
	}
	insts, err := s.store.InstancesByIDs(r.Context(), instanceIDs)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	windows, _ := s.store.ListSyncWindows(r.Context())
	statuses, err := s.appsForInstances(r.Context(), insts, windows)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	instByID := make(map[string]model.Instance, len(insts))
	for _, inst := range insts {
		instByID[inst.ID] = s.decorateInstance(inst)
	}
	statusByKey := map[string]AppStatus{}
	for instID, list := range statuses {
		for _, status := range list {
			statusByKey[instID+"/"+strconv.FormatInt(status.Application.ID, 10)] = status
		}
	}
	rows := make([]DriftRow, 0, len(keys))
	for _, key := range keys {
		inst, ok := instByID[key.InstanceID]
		if !ok {
			continue
		}
		status, ok := statusByKey[key.InstanceID+"/"+strconv.FormatInt(key.AppID, 10)]
		if !ok {
			continue
		}
		rows = append(rows, DriftRow{Instance: inst, App: status})
	}
	facets := model.AssignmentFacets{Projects: []string{}, Apps: []model.AppRef{}}
	if r.URL.Query().Get("facets") == "1" {
		facets, err = s.store.DriftAssignmentFacets(r.Context(), all, scopes)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, struct {
		Items  []DriftRow             `json:"items"`
		Total  int                    `json:"total"`
		Limit  int                    `json:"limit"`
		Offset int                    `json:"offset"`
		Stats  model.AssignmentStats  `json:"stats"`
		Facets model.AssignmentFacets `json:"facets"`
	}{Items: rows, Total: total, Limit: limit, Offset: offset, Stats: stats, Facets: facets})
}

const overviewRecentLimit = 9

// handleOverview returns the dashboard's aggregate view in a bounded number of
// queries, so opening the overview does not page through the entire fleet.
func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	all, scopes := s.visibleProjects(r, "instances", "get")
	now := time.Now().UTC()
	stats, err := s.store.InstanceOverviewStats(r.Context(), all, scopes, now)
	if err != nil {
		s.internalError(w, r, err)
		return
	}

	runs, err := s.store.ListRunsScoped(r.Context(), all, scopes, []string{"check", "apply"}, 100)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	instanceIDs := make([]string, 0, len(runs))
	appIDs := make([]int64, 0, len(runs))
	seenID := map[string]bool{}
	seenApp := map[int64]bool{}
	for _, run := range runs {
		if !seenID[run.InstanceID] {
			seenID[run.InstanceID] = true
			instanceIDs = append(instanceIDs, run.InstanceID)
		}
		if !seenApp[run.AppID] {
			seenApp[run.AppID] = true
			appIDs = append(appIDs, run.AppID)
		}
	}
	insts, err := s.store.InstancesByIDs(r.Context(), instanceIDs)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	applications, err := s.store.ApplicationsByIDs(r.Context(), appIDs)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	instByID := make(map[string]model.Instance, len(insts))
	for _, inst := range insts {
		instByID[inst.ID] = s.decorateInstance(inst)
	}
	appByID := make(map[int64]model.Application, len(applications))
	for _, app := range applications {
		appByID[app.ID] = app
	}
	recent := []model.OverviewRecent{}
	seenAssignment := map[string]bool{}
	for _, run := range runs {
		inst, ok := instByID[run.InstanceID]
		if !ok {
			continue
		}
		app, ok := appByID[run.AppID]
		if !ok {
			continue
		}
		key := run.InstanceID + "/" + strconv.FormatInt(run.AppID, 10)
		if seenAssignment[key] {
			continue
		}
		seenAssignment[key] = true
		recent = append(recent, model.OverviewRecent{
			Instance:    inst,
			Application: model.AppRef{ID: app.ID, Name: app.Name},
			Run:         run,
		})
		if len(recent) == overviewRecentLimit {
			break
		}
	}

	base := model.InstanceListFilter{AllProjects: all, Projects: scopes, Now: now}
	attention, err := s.overviewAttention(r.Context(), base)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Stats     model.OverviewStats    `json:"stats"`
		Recent    []model.OverviewRecent `json:"recent"`
		Attention []model.AttentionItem  `json:"attention"`
	}{Stats: stats, Recent: recent, Attention: attention})
}

func (s *Server) overviewAttention(ctx context.Context, base model.InstanceListFilter) ([]model.AttentionItem, error) {
	out := []model.AttentionItem{}
	offline := base
	offline.State = model.StateOffline
	offline.RecentFirst = true
	offline.Limit = 30
	offlineInsts, err := s.store.ListInstancesFiltered(ctx, offline)
	if err != nil {
		return nil, err
	}
	for _, inst := range offlineInsts {
		out = append(out, model.AttentionItem{Kind: model.StateOffline, Instance: s.decorateInstance(inst), At: inst.LastSeen})
	}

	issueByID := map[string]model.Instance{}
	for _, filter := range []model.InstanceListFilter{
		{State: model.StateError, RecentFirst: true, Limit: 30},
		{State: model.StateDrift, RecentFirst: true, Limit: 30},
		{StuckOnly: true, RecentFirst: true, Limit: 30},
	} {
		filter.AllProjects = base.AllProjects
		filter.Projects = base.Projects
		filter.Now = base.Now
		list, err := s.store.ListInstancesFiltered(ctx, filter)
		if err != nil {
			return nil, err
		}
		for _, inst := range list {
			issueByID[inst.ID] = inst
		}
	}
	issueInsts := make([]model.Instance, 0, len(issueByID))
	for _, inst := range issueByID {
		issueInsts = append(issueInsts, inst)
	}
	if len(issueInsts) > 0 {
		windows, _ := s.store.ListSyncWindows(ctx)
		statuses, err := s.appsForInstances(ctx, issueInsts, windows)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, inst := range issueInsts {
			inst = s.decorateInstance(inst)
			if !inst.Online {
				continue
			}
			for _, status := range statuses[inst.ID] {
				kind := ""
				at := status.LatestCheck.FinishedAt
				switch {
				case status.LatestCheck.Status == model.RunFailed:
					kind = "failed"
				case status.Stuck:
					kind = "stuck"
					if !status.StuckAt.IsZero() {
						at = status.StuckAt
					}
				case status.LatestCheck.Status == model.RunDrift:
					kind = "drift"
				default:
					continue
				}
				key := kind + "/" + inst.ID + "/" + strconv.FormatInt(status.Application.ID, 10)
				if seen[key] {
					continue
				}
				seen[key] = true
				app := model.AppRef{ID: status.Application.ID, Name: status.Application.Name}
				out = append(out, model.AttentionItem{Kind: kind, Instance: inst, Application: &app, At: at})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].At.IsZero() {
			return false
		}
		if out[j].At.IsZero() {
			return true
		}
		return out[i].At.After(out[j].At)
	})
	if len(out) > 7 {
		out = out[:7]
	}
	return out, nil
}

type instanceRunKey struct {
	instanceID string
	appID      int64
	mode       string
}

func (s *Server) appsForInstances(ctx context.Context, insts []model.Instance, windows []model.SyncWindow) (map[string][]AppStatus, error) {
	out := map[string][]AppStatus{}
	if len(insts) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(insts))
	for _, inst := range insts {
		ids = append(ids, inst.ID)
	}
	assignments, err := s.store.ListAssignmentsWithApplications(ctx, ids)
	if err != nil {
		return nil, err
	}
	runs, err := s.store.ListLatestRuns(ctx, ids, []string{"check", "apply"})
	if err != nil {
		return nil, err
	}
	latest := make(map[instanceRunKey]model.Run, len(runs))
	for _, run := range runs {
		latest[instanceRunKey{instanceID: run.InstanceID, appID: run.AppID, mode: run.Mode}] = run
	}
	hosts := make(map[string]model.Instance, len(insts))
	for _, inst := range insts {
		hosts[inst.ID] = inst
	}
	now := time.Now()
	for _, row := range assignments {
		inst, ok := hosts[row.Assignment.InstanceID]
		if !ok {
			continue
		}
		a := row.Assignment
		app := row.Application
		as := AppStatus{
			Application:    app,
			AutoHeal:       effectiveAutoHeal(inst.AutoHeal, app, a),
			HostAutoHeal:   inst.AutoHeal,
			Stuck:          a.Stuck,
			StuckAt:        a.StuckAt,
			LastCheckAt:    a.LastCheckAt,
			TargetRevision: a.TargetRevision,
			Frozen:         syncwindows.Frozen(windows, app.ID, now),
		}
		if a.AutoHeal != nil {
			as.AutoHealSet = true
		}
		if check, ok := latest[instanceRunKey{instanceID: inst.ID, appID: a.AppID, mode: "check"}]; ok {
			as.LatestCheck = check
			as.HasCheck = true
		}
		if apply, ok := latest[instanceRunKey{instanceID: inst.ID, appID: a.AppID, mode: "apply"}]; ok {
			as.LatestApply = apply
		}
		out[inst.ID] = append(out[inst.ID], as)
	}
	return out, nil
}

// effectiveAutoHeal combines the three control levels with AND semantics: the
// host switch, the application default, and the per-(host, app) override must
// all allow auto-apply. A nil override inherits (enabled).
func effectiveAutoHeal(hostAutoHeal bool, app model.Application, a model.Assignment) bool {
	if !hostAutoHeal || !app.AutoHeal {
		return false
	}
	if a.AutoHeal != nil {
		return *a.AutoHeal
	}
	return true
}

const (
	maxInstanceTags    = 64
	maxInstanceTagSize = 64
)

// normalizeInstanceTags trims, de-duplicates (case-insensitively) and caps
// operator labels before they are persisted.
func normalizeInstanceTags(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		tag := strings.TrimSpace(raw)
		if tag == "" {
			continue
		}
		if r := []rune(tag); len(r) > maxInstanceTagSize {
			tag = string(r[:maxInstanceTagSize])
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tag)
		if len(out) == maxInstanceTags {
			break
		}
	}
	return out
}

// handleCreateInstance registers a new instance and returns its bootstrap token once.
func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name          string   `json:"name"`
		Project       string   `json:"project"`
		Region        string   `json:"region"`
		Tags          []string `json:"tags"`
		ExecutionMode string   `json:"execution_mode"`
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
	body.Region = strings.TrimSpace(body.Region)
	body.Tags = normalizeInstanceTags(body.Tags)
	body.ExecutionMode = strings.TrimSpace(body.ExecutionMode)
	if body.ExecutionMode == "" {
		body.ExecutionMode = model.ModeAgent
	}
	if body.ExecutionMode != model.ModeAgent && body.ExecutionMode != model.ModeTunnel {
		writeErr(w, http.StatusBadRequest, "execution_mode must be agent|tunnel")
		return
	}
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
	tunnelPort := 0
	if body.ExecutionMode == model.ModeTunnel {
		var err error
		tunnelPort, err = s.allocateTunnelPort(r.Context())
		if err != nil {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
	}
	token, err := auth.NewToken()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	inst, err := s.store.CreateInstance(r.Context(), model.Instance{
		ID:            genInstanceID(),
		Name:          body.Name,
		Project:       body.Project,
		Region:        body.Region,
		Tags:          body.Tags,
		ExecutionMode: body.ExecutionMode,
		TunnelPort:    tunnelPort,
		TokenHash:     auth.Hash(token),
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"instance": s.decorateInstance(inst), "token": token})
}

// allocateTunnelPort returns the first free loopback port in the configured
// range. Tunnel instances are few and created by operators, so a simple scan
// is enough (and avoids racing with an unused port allocator table).
func (s *Server) allocateTunnelPort(ctx context.Context) (int, error) {
	insts, err := s.store.ListTunnelInstances(ctx)
	if err != nil {
		return 0, err
	}
	used := make(map[int]bool, len(insts))
	for _, inst := range insts {
		if inst.TunnelPort > 0 {
			used[inst.TunnelPort] = true
		}
	}
	min, max := s.tunnelPortMin, s.tunnelPortMax
	if min <= 0 {
		min = 22000
	}
	if max < min {
		max = min + 999
	}
	for p := min; p <= max; p++ {
		if !used[p] {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no free tunnel port in range %d-%d", min, max)
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
	inst = s.decorateInstance(inst)
	windows, _ := s.store.ListSyncWindows(r.Context())
	apps, err := s.appsForInstances(r.Context(), []model.Instance{inst}, windows)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	as := apps[inst.ID]
	if as == nil {
		as = []AppStatus{}
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

// handleUpdateInstance renames an instance or moves it to another project.
// Moving is refused while applications are assigned so project scoping stays
// consistent; unassign them first.
func (s *Server) handleUpdateInstance(w http.ResponseWriter, r *http.Request) {
	id, _ := idParam(r, "id")
	var body struct {
		Name    string    `json:"name"`
		Project *string   `json:"project"`
		Region  *string   `json:"region"`
		Tags    *[]string `json:"tags"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	inst, err := s.store.GetInstance(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	if !s.requireObject(w, r, "instances.update", inst.Project) {
		return
	}
	project := inst.Project
	if body.Project != nil {
		project = strings.TrimSpace(*body.Project)
	}
	region := inst.Region
	if body.Region != nil {
		region = strings.TrimSpace(*body.Region)
	}
	tags := inst.Tags
	if body.Tags != nil {
		tags = normalizeInstanceTags(*body.Tags)
	}
	if project != inst.Project {
		if !s.requireObject(w, r, "instances.update", project) {
			return
		}
		if msg, err := s.validateProjectOnly(r.Context(), project); err != nil {
			s.internalError(w, r, err)
			return
		} else if msg != "" {
			writeErr(w, http.StatusBadRequest, msg)
			return
		}
		ass, err := s.store.ListAssignments(r.Context(), id)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		if len(ass) > 0 {
			writeErr(w, http.StatusConflict, "unassign applications before moving the instance to another project")
			return
		}
	}
	if err := s.store.UpdateInstanceMeta(r.Context(), id, name, project, region, tags); err != nil {
		s.internalError(w, r, err)
		return
	}
	inst.Name = name
	inst.Project = project
	inst.Region = region
	inst.Tags = tags
	writeJSON(w, http.StatusOK, s.decorateInstance(inst))
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
	instanceIDs := make([]string, 0, len(runs))
	seen := map[string]bool{}
	for i := range runs {
		runs[i].OutputTail = ""
		runs[i].ChangedTasks = nil
		runs[i].Summary = nil
		if !seen[runs[i].InstanceID] {
			seen[runs[i].InstanceID] = true
			instanceIDs = append(instanceIDs, runs[i].InstanceID)
		}
	}
	insts, err := s.store.InstancesByIDs(r.Context(), instanceIDs)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	names := make(map[string]string, len(insts))
	for _, inst := range insts {
		names[inst.ID] = inst.Name
	}
	writeJSON(w, http.StatusOK, struct {
		Items         []model.Run       `json:"items"`
		Total         int               `json:"total"`
		Limit         int               `json:"limit"`
		Offset        int               `json:"offset"`
		InstanceNames map[string]string `json:"instance_names"`
	}{Items: runs, Total: total, Limit: limit, Offset: offset, InstanceNames: names})
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
	q := r.URL.Query()
	filter := model.AuditFilter{
		Query:  strings.TrimSpace(q.Get("q")),
		Method: strings.TrimSpace(q.Get("method")),
		Status: strings.TrimSpace(q.Get("status")),
		User:   strings.TrimSpace(q.Get("user")),
		Limit:  limit,
		Offset: offset,
	}
	if v := strings.TrimSpace(q.Get("from")); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid from date")
			return
		}
		filter.Since = t
	}
	if v := strings.TrimSpace(q.Get("to")); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid to date")
			return
		}
		filter.Until = t.Add(24*time.Hour - time.Nanosecond)
	}
	if !filter.Since.IsZero() && !filter.Until.IsZero() && filter.Since.After(filter.Until) {
		writeErr(w, http.StatusBadRequest, "from date must be before to date")
		return
	}
	entries, total, err := s.store.ListAudit(r.Context(), filter)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	facets, err := s.store.AuditFacets(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Items  []model.AuditEntry `json:"items"`
		Total  int                `json:"total"`
		Limit  int                `json:"limit"`
		Offset int                `json:"offset"`
		Facets model.AuditFacets  `json:"facets"`
	}{Items: entries, Total: total, Limit: limit, Offset: offset, Facets: facets})
}

// ---- sync windows (operator) ----

func (s *Server) handleListSyncWindows(w http.ResponseWriter, r *http.Request) {
	all, scopes := s.visibleProjects(r, "windows", "get")
	out, err := s.store.ListSyncWindows(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if !all {
		apps, err := s.store.ListApplicationsScoped(r.Context(), scopes)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		visibleApps := make(map[int64]bool, len(apps))
		for _, app := range apps {
			visibleApps[app.ID] = true
		}
		filtered := out[:0]
		for _, w := range out {
			// Global windows (app_id=0) affect every project and are safe to
			// expose; app-specific windows only when their app is visible.
			if w.AppID == 0 || visibleApps[w.AppID] {
				filtered = append(filtered, w)
			}
		}
		out = filtered
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

func (s *Server) handleGetApplication(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, app)
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
	if _, err := s.store.EnsureRepositoryByProjectURL(r.Context(), in.Project, in.RepoURL); err != nil {
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
	if _, err := s.store.EnsureRepositoryByProjectURL(r.Context(), in.Project, in.RepoURL); err != nil {
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

// handleSetInstanceAutoHeal toggles the host-level auto-apply switch. When
// disabled, no application on the host is applied automatically; drift checks
// continue. Per-(host, app) overrides still apply once the host is re-enabled.
func (s *Server) handleSetInstanceAutoHeal(w http.ResponseWriter, r *http.Request) {
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
	if !s.requireObject(w, r, "instances.update", inst.Project) {
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if err := s.store.SetInstanceAutoHeal(r.Context(), id, body.Enabled); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": body.Enabled})
}

// handleSetApplicationAutoHeal toggles auto-apply on drift for every instance
// that inherits the application default (per-instance overrides still apply).
func (s *Server) handleSetApplicationAutoHeal(w http.ResponseWriter, r *http.Request) {
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
	if !s.requireObject(w, r, "applications.update", app.Project) {
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if err := s.store.SetApplicationAutoHeal(r.Context(), id, body.Enabled); err != nil {
		s.internalError(w, r, err)
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
	Project            string    `json:"project"`
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
		Project:            r.Project,
		Username:           r.Username,
		PasswordSet:        r.Password != "",
		SSHKeySet:          r.SSHKey != "",
		InsecureSkipVerify: r.InsecureSkipVerify,
		CreatedAt:          r.CreatedAt,
	}
}

func (s *Server) handleListRepositories(w http.ResponseWriter, r *http.Request) {
	all, scopes := s.visibleProjects(r, "repositories", "get")
	repos, err := s.store.ListRepositories(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if !all {
		visible := make(map[string]bool, len(scopes))
		for _, scope := range scopes {
			visible[scope] = true
		}
		filtered := repos[:0]
		for _, repo := range repos {
			if visible[repo.Project] {
				filtered = append(filtered, repo)
			}
		}
		repos = filtered
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
	in.Project = strings.TrimSpace(in.Project)
	if !s.requireObject(w, r, "repositories.create", in.Project) {
		return
	}
	if msg, err := s.validateAppProject(r.Context(), in.Project, in.URL); err != nil {
		s.internalError(w, r, err)
		return
	} else if msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if in.Name == "" {
		in.Name = in.URL
	}
	repo, err := s.store.CreateRepository(r.Context(), in)
	if err != nil {
		if errors.Is(err, store.ErrConflict) || strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, http.StatusConflict, "repository url already exists in this project")
			return
		}
		s.internalError(w, r, err)
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
	if !s.requireObject(w, r, "repositories.update", old.Project) {
		return
	}
	raw, err := readRawJSON(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	var in model.Repository
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
	if _, ok := present["project"]; !ok {
		in.Project = old.Project
	}
	in.Project = strings.TrimSpace(in.Project)
	if in.Project != old.Project && !s.requireObject(w, r, "repositories.update", in.Project) {
		return
	}
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
	if _, ok := present["insecure_skip_verify"]; !ok {
		in.InsecureSkipVerify = old.InsecureSkipVerify
	}
	in.CreatedAt = old.CreatedAt

	if in.Project != old.Project || in.URL != old.URL {
		apps, err := s.store.CountApplicationsByProjectRepoURL(r.Context(), old.Project, old.URL)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		if apps > 0 {
			writeErr(w, http.StatusConflict, fmt.Sprintf(
				"repository is still used by %d application(s); cannot change its project or URL",
				apps))
			return
		}
	}
	if msg, err := s.validateAppProject(r.Context(), in.Project, in.URL); err != nil {
		s.internalError(w, r, err)
		return
	} else if msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdateRepository(r.Context(), in); err != nil {
		if errors.Is(err, store.ErrConflict) || strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, http.StatusConflict, "repository url already exists in this project")
			return
		}
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toRepoView(in))
}

func (s *Server) handleCheckRepository(w http.ResponseWriter, r *http.Request) {
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
	if !s.requireObject(w, r, "repositories.get", repo.Project) {
		return
	}
	err = gitops.Check(r.Context(), repo.URL, gitops.Creds{
		Username:           repo.Username,
		Password:           repo.Password,
		SSHKey:             repo.SSHKey,
		InsecureSkipVerify: repo.InsecureSkipVerify,
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, "repository connection failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteRepository(w http.ResponseWriter, r *http.Request) {
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
	if !s.requireObject(w, r, "repositories.delete", repo.Project) {
		return
	}
	apps, err := s.store.CountApplicationsByProjectRepoURL(r.Context(), repo.Project, repo.URL)
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
	InstanceID string         `json:"instance_id"`
	Version    string         `json:"version"`
	Hostname   string         `json:"hostname"`
	Platform   string         `json:"platform"`
	HostInfo   model.HostInfo `json:"host_info"`
	// PollInterval is the agent heartbeat period in seconds (0 for older
	// agents that predate this field).
	PollInterval int64 `json:"poll_interval"`
}

type checkinResponse struct {
	Applications  []model.Application `json:"applications"`
	Commands      []model.Command     `json:"commands"`
	RepoCache     bool                `json:"repo_cache"`
	InstanceID    string              `json:"instance_id"`
	ScanInterval  int64               `json:"scan_interval"`
	JitterPercent int                 `json:"jitter_percent"`
}

// heartbeatPayload is a liveness-only ping. It must not claim commands or
// return assignments: the agent sends it from a dedicated goroutine while a
// long check may still be running.
type heartbeatPayload struct {
	// InstanceID is accepted (and ignored) because the agent reuses its
	// checkin payload for the lightweight heartbeat.
	InstanceID   string         `json:"instance_id"`
	Version      string         `json:"version"`
	Hostname     string         `json:"hostname"`
	Platform     string         `json:"platform"`
	HostInfo     model.HostInfo `json:"host_info"`
	PollInterval int64          `json:"poll_interval"`
}

// saveHostInfo persists best-effort inventory without failing the liveness
// request when the inventory write fails.
func (s *Server) saveHostInfo(ctx context.Context, id string, info model.HostInfo) {
	if info.Empty() {
		return
	}
	if err := s.store.SetInstanceHostInfo(ctx, id, info); err != nil {
		s.log.Warn("failed to save host info", "instance", id, "error", err)
	}
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var body heartbeatPayload
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	inst := r.Context().Value(ctxInstance).(model.Instance)
	poll := int64(0)
	if body.PollInterval > 0 && body.PollInterval <= 86400 {
		poll = body.PollInterval
	}
	if err := s.store.TouchInstance(r.Context(), inst.ID, body.Version, body.Hostname, body.Platform,
		poll, time.Now().UTC(), inst.ExecutionMode == model.ModeTunnel); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.saveHostInfo(r.Context(), inst.ID, body.HostInfo)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
	// Heartbeat: update liveness metadata without rewriting state, so a
	// concurrent lightweight heartbeat and a full checkin cannot clobber each
	// other's columns.
	poll := int64(0)
	if body.PollInterval > 0 && body.PollInterval <= 86400 {
		poll = body.PollInterval
	}
	inst.Version = body.Version
	inst.Hostname = body.Hostname
	inst.Platform = body.Platform
	if err := s.store.TouchInstance(r.Context(), inst.ID, body.Version, body.Hostname, body.Platform,
		poll, time.Now().UTC(), inst.ExecutionMode == model.ModeTunnel); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.saveHostInfo(r.Context(), inst.ID, body.HostInfo)

	windows, _ := s.store.ListSyncWindows(r.Context())
	metrics.Checkin(inst.ID)
	scanning, err := s.scanningSettings(r.Context())
	if err != nil {
		s.log.Warn("failed to load scan policy; using defaults", "error", err)
		scanning = scanningSettings{IntervalSeconds: defaultScanSeconds, JitterPercent: defaultScanJitter}
	}
	resp := checkinResponse{
		RepoCache:     s.repoCache != nil,
		InstanceID:    inst.ID,
		ScanInterval:  scanning.IntervalSeconds,
		JitterPercent: scanning.JitterPercent,
	}
	assignments, err := s.store.ListAssignmentsWithApplications(r.Context(), []string{inst.ID})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	appIDs := make([]int64, 0, len(assignments))
	urls := make([]string, 0, len(assignments))
	for _, row := range assignments {
		appIDs = append(appIDs, row.Assignment.AppID)
		urls = append(urls, row.Application.RepoURL)
	}
	repos, repoErr := s.store.RepositoriesByProjectURLs(r.Context(), inst.Project, urls)
	if repoErr != nil {
		s.log.Warn("failed to load repositories for checkin", "instance", inst.ID, "error", repoErr)
		repos = map[string]model.Repository{}
	}
	appVars, appVarsErr := s.store.ListAppVariablesForApps(r.Context(), appIDs)
	if appVarsErr != nil {
		s.log.Warn("failed to load app variables for checkin", "instance", inst.ID, "error", appVarsErr)
		appVars = map[int64][]model.Variable{}
	}
	hostVars, hostVarsErr := s.store.ListAssignmentVariablesForApps(r.Context(), inst.ID, appIDs)
	if hostVarsErr != nil {
		s.log.Warn("failed to load host variables for checkin", "instance", inst.ID, "error", hostVarsErr)
		hostVars = map[int64][]model.Variable{}
	}
	for _, row := range assignments {
		a := row.Assignment
		app := row.Application
		app.AutoHeal = effectiveAutoHeal(inst.AutoHeal, app, a)
		// Rollback/pinned revision wins over the application branch. The agent
		// receives it as the target revision and tells a commit SHA from a
		// branch/tag so a pinned rollback can still clone and check out.
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
		if repo, ok := repos[app.RepoURL]; ok {
			app.Connection = &repo
		}
		// Merge: legacy vars < app defaults < this host's overrides.
		if merged, err := mergeVarsJSON(app.Vars, appVars[a.AppID], hostVars[a.AppID]); err == nil {
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

// handlePendingCommands is a cheap probe used by the agent's fast command
// watcher. It does not claim commands or refresh liveness; when true, the agent
// wakes its main loop immediately and performs a full checkin.
func (s *Server) handlePendingCommands(w http.ResponseWriter, r *http.Request) {
	inst := r.Context().Value(ctxInstance).(model.Instance)
	cmds, err := s.store.PendingCommands(r.Context(), inst.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"pending": len(cmds) > 0,
		"count":   len(cmds),
	})
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
				if a, err := s.store.GetAssignment(r.Context(), inst.ID, body.AppID); err == nil && effectiveAutoHeal(inst.AutoHeal, app, a) {
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
	s.publishEvent(hubEvent{
		Type: "run", InstanceID: inst.ID, AppID: body.AppID,
		Mode: body.Mode, Status: body.Status, Trigger: body.Trigger,
	})
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
	runs, err := s.store.ListLatestRuns(ctx, []string{inst.ID}, []string{"check"})
	if err != nil {
		return err
	}
	latestByApp := make(map[int64]model.Run, len(runs))
	for _, run := range runs {
		latestByApp[run.AppID] = run
	}
	for _, a := range ass {
		latest, ok := latestByApp[a.AppID]
		if !ok {
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
	return s.store.UpdateInstanceState(ctx, inst.ID, state, inst.LastCommit)
}
