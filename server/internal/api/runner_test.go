package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/store"
)

func createInstance(t *testing.T, st store.Store, id, mode, inventoryHost string) model.Instance {
	t.Helper()
	inst, err := st.CreateInstance(context.Background(), model.Instance{
		ID:            id,
		Name:          id,
		TokenHash:     "hash",
		ExecutionMode: mode,
		InventoryHost: inventoryHost,
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	return inst
}

func TestCreateSSHInstanceStoresInventoryHost(t *testing.T) {
	s, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances",
		strings.NewReader(`{"name":"edge-01","project":"","execution_mode":"ssh","inventory_host":"edge-01.internal","tags":["prod"," team-a ","PROD",""]}`))
	rec := httptest.NewRecorder()
	s.handleCreateInstance(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status %d body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Instance model.Instance `json:"instance"`
		Token    string         `json:"token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("expected a bootstrap token")
	}
	if resp.Instance.ExecutionMode != model.ModeSSH {
		t.Fatalf("mode = %q, want ssh", resp.Instance.ExecutionMode)
	}
	if resp.Instance.InventoryHost != "edge-01.internal" {
		t.Fatalf("inventory_host = %q", resp.Instance.InventoryHost)
	}
	if want := []string{"prod", "team-a"}; !slices.Equal(resp.Instance.Tags, want) {
		t.Fatalf("tags = %v, want %v", resp.Instance.Tags, want)
	}

	// ssh instances require an inventory host.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/instances",
		strings.NewReader(`{"name":"edge-02","project":"","execution_mode":"ssh"}`))
	rec = httptest.NewRecorder()
	s.handleCreateInstance(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing host: status %d, want 400", rec.Code)
	}
}

func TestRunnerAPIRequiresToken(t *testing.T) {
	s, st := newTestServer(t)
	createInstance(t, st, "t1", model.ModeSSH, "t1.internal")

	// Disabled until a token is configured.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runner/instances", nil)
	rec := httptest.NewRecorder()
	s.runnerAuth(s.handleRunnerInstances)(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled: status %d, want 503", rec.Code)
	}

	s.SetRunnerToken("runner-secret")
	rec = httptest.NewRecorder()
	s.runnerAuth(s.handleRunnerInstances)(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing bearer: status %d, want 401", rec.Code)
	}

	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	s.runnerAuth(s.handleRunnerInstances)(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad bearer: status %d, want 401", rec.Code)
	}

	req.Header.Set("Authorization", "Bearer runner-secret")
	rec = httptest.NewRecorder()
	s.runnerAuth(s.handleRunnerInstances)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authorized: status %d body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"inventory_host":"t1.internal"`) {
		t.Fatalf("response misses inventory host: %s", rec.Body.String())
	}
}

func TestRunnerInstanceAuthRejectsAgentMode(t *testing.T) {
	s, st := newTestServer(t)
	s.SetRunnerToken("runner-secret")
	createInstance(t, st, "agent-1", model.ModeAgent, "")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/runner/instances/agent-1/checkin",
		strings.NewReader(`{}`))
	req.SetPathValue("id", "agent-1")
	req.Header.Set("Authorization", "Bearer runner-secret")
	rec := httptest.NewRecorder()
	s.runnerInstanceAuth(s.handleCheckin)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", rec.Code)
	}
}

func TestSSHCheckinUpdatesLiveness(t *testing.T) {
	s, st := newTestServer(t)
	s.SetRunnerToken("runner-secret")
	createInstance(t, st, "ssh-1", model.ModeSSH, "ssh-1.internal")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/runner/instances/ssh-1/checkin",
		strings.NewReader(`{"version":"0.1.1","hostname":"edge-01","platform":"ssh"}`))
	req.SetPathValue("id", "ssh-1")
	req.Header.Set("Authorization", "Bearer runner-secret")
	rec := httptest.NewRecorder()
	s.runnerInstanceAuth(s.handleCheckin)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("checkin: status %d body %s", rec.Code, rec.Body.String())
	}

	got, err := st.GetInstance(context.Background(), "ssh-1")
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	if got.LastSeen.IsZero() {
		t.Fatal("last_seen was not set")
	}
	if time.Since(got.LastSeen) > time.Minute {
		t.Fatalf("last_seen too old: %v", got.LastSeen)
	}
	if got.Hostname != "edge-01" || got.Platform != "ssh" {
		t.Fatalf("hostname/platform = %q/%q", got.Hostname, got.Platform)
	}
}

func TestOfflineGrace(t *testing.T) {
	cases := []struct {
		poll int64
		want time.Duration
	}{
		{0, defaultOfflineGrace}, // older agent: conservative default
		{1, minOfflineGrace},     // floor for very fast polls
		{5, minOfflineGrace},     // demo agents
		{60, 3 * time.Minute},    // three missed beats
		{3600, maxOfflineGrace},  // cap for very slow polls
	}
	for _, c := range cases {
		if got := offlineGrace(model.Instance{PollInterval: c.poll}); got != c.want {
			t.Errorf("offlineGrace(%d) = %v, want %v", c.poll, got, c.want)
		}
	}
}

func TestDecorateInstanceMarksSilentAgentOffline(t *testing.T) {
	s := &Server{}
	now := time.Now()

	fresh := s.decorateInstance(model.Instance{
		LastSeen: now.Add(-10 * time.Second), PollInterval: 60, State: model.StateOK,
	})
	if !fresh.Online || fresh.State != model.StateOK {
		t.Fatalf("fresh agent online=%v state=%q, want online ok", fresh.Online, fresh.State)
	}

	stale := s.decorateInstance(model.Instance{
		LastSeen: now.Add(-4 * time.Minute), PollInterval: 60, State: model.StateOK,
	})
	if stale.Online || stale.State != model.StateOffline {
		t.Fatalf("stale agent online=%v state=%q, want offline", stale.Online, stale.State)
	}

	never := s.decorateInstance(model.Instance{State: model.StateUnknown})
	if never.Online || never.State != model.StateUnknown {
		t.Fatalf("never-seen agent online=%v state=%q, want unknown", never.Online, never.State)
	}
}

func TestHeartbeatUpdatesLivenessWithoutState(t *testing.T) {
	s, st := newTestServer(t)
	inst := createInstance(t, st, "hb-1", model.ModeAgent, "")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/heartbeat",
		strings.NewReader(`{"instance_id":"hb-1","version":"9.9","hostname":"hb","platform":"linux","poll_interval":7}`))
	req = req.WithContext(context.WithValue(req.Context(), ctxInstance, inst))
	rec := httptest.NewRecorder()
	s.handleHeartbeat(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("heartbeat: status %d body %s", rec.Code, rec.Body.String())
	}

	got, err := st.GetInstance(context.Background(), "hb-1")
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	if got.Version != "9.9" || got.Hostname != "hb" || got.Platform != "linux" {
		t.Fatalf("liveness metadata = %q/%q/%q", got.Version, got.Hostname, got.Platform)
	}
	if got.PollInterval != 7 {
		t.Fatalf("poll_interval = %d, want 7", got.PollInterval)
	}
	if got.LastSeen.IsZero() {
		t.Fatal("last_seen was not set")
	}
	if got.State != model.StateUnknown {
		t.Fatalf("heartbeat changed state to %q", got.State)
	}
}

func TestUpdateInstanceRenamesWithoutTouchingLiveness(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	createInstance(t, st, "ren-1", model.ModeAgent, "")
	if err := st.TouchInstance(ctx, "ren-1", "1.2.3", "h1", "linux", 5, time.Now().UTC()); err != nil {
		t.Fatalf("touch instance: %v", err)
	}
	before, err := st.GetInstance(ctx, "ren-1")
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/instances/ren-1", strings.NewReader(`{"name":"renamed","region":"eu-west-1","tags":["prod","team-a","prod"]}`))
	req.SetPathValue("id", "ren-1")
	rec := httptest.NewRecorder()
	s.handleUpdateInstance(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update instance: status %d body %s", rec.Code, rec.Body.String())
	}

	got, err := st.GetInstance(ctx, "ren-1")
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	if got.Name != "renamed" {
		t.Fatalf("name = %q, want renamed", got.Name)
	}
	if got.Region != "eu-west-1" {
		t.Fatalf("region = %q, want eu-west-1", got.Region)
	}
	if want := []string{"prod", "team-a"}; !slices.Equal(got.Tags, want) {
		t.Fatalf("tags = %v, want %v", got.Tags, want)
	}
	if got.PollInterval != before.PollInterval || !got.LastSeen.Equal(before.LastSeen) {
		t.Fatalf("rename touched liveness: poll %d->%d last_seen %v->%v",
			before.PollInterval, got.PollInterval, before.LastSeen, got.LastSeen)
	}
}

func TestSSHCheckinReturnsScanPolicy(t *testing.T) {
	s, st := newTestServer(t)
	s.SetRunnerToken("runner-secret")
	createInstance(t, st, "ssh-scan", model.ModeSSH, "ssh-scan.internal")
	if err := st.SetRuntimeSetting(context.Background(), scanIntervalSetting, "15"); err != nil {
		t.Fatalf("set interval: %v", err)
	}
	if err := st.SetRuntimeSetting(context.Background(), scanJitterSetting, "35"); err != nil {
		t.Fatalf("set jitter: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/runner/instances/ssh-scan/checkin",
		strings.NewReader(`{}`))
	req.SetPathValue("id", "ssh-scan")
	req.Header.Set("Authorization", "Bearer runner-secret")
	rec := httptest.NewRecorder()
	s.runnerInstanceAuth(s.handleCheckin)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("checkin: status %d body %s", rec.Code, rec.Body.String())
	}
	var got checkinResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ScanInterval != 15 || got.JitterPercent != 35 {
		t.Fatalf("scan policy = %d/%d, want 15/35", got.ScanInterval, got.JitterPercent)
	}
	if got.InstanceID != "ssh-scan" {
		t.Fatalf("instance id = %q, want ssh-scan", got.InstanceID)
	}
}

func TestRunnerInventoryListsSSHInstancesWithoutToken(t *testing.T) {
	s, st := newTestServer(t)
	createInstance(t, st, "ssh-1", model.ModeSSH, "ssh-1.internal")
	s.SetRunnerToken("runner-secret")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runners", nil)
	rec := httptest.NewRecorder()
	s.handleRunnerInventory(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "runner-secret") {
		t.Fatalf("runner token leaked in response: %s", rec.Body.String())
	}
	var resp runnerInventory
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Enabled {
		t.Fatalf("enabled = %v, want true", resp.Enabled)
	}
	if resp.Summary.Instances != 1 || resp.Summary.Online != 0 {
		t.Fatalf("summary = %+v, want one offline instance", resp.Summary)
	}
	if len(resp.Instances) != 1 || resp.Instances[0].ID != "ssh-1" {
		t.Fatalf("instances = %+v, want ssh-1 only", resp.Instances)
	}
	if resp.Instances[0].InventoryHost != "ssh-1.internal" {
		t.Fatalf("instance inventory host = %q", resp.Instances[0].InventoryHost)
	}
}

func TestCheckinHubInventoryOnlyForSSH(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	sshInst := createInstance(t, st, "ssh-hub", model.ModeSSH, "web-1")
	agentInst, err := st.CreateInstance(ctx, model.Instance{
		ID: "agent-hub", Name: "agent-hub", TokenHash: "hash-agent", ExecutionMode: model.ModeAgent,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	app, err := st.CreateApplication(ctx, model.Application{
		Name: "hub-inv", RepoURL: "https://example.com/repo.git", Branch: "main", Path: "site.yml",
		InventorySource: model.InventorySourceHub,
		InventoryData:   `{"groups":[{"name":"web","hosts":[{"name":"web-1","vars":{"ansible_password":"hunter2"}}]}]}`,
		Vars:            "{}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Assign(ctx, sshInst.ID, app.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.Assign(ctx, agentInst.ID, app.ID); err != nil {
		t.Fatal(err)
	}

	checkin := func(inst model.Instance) checkinResponse {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/checkin", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), ctxInstance, inst))
		rec := httptest.NewRecorder()
		s.handleCheckin(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("checkin %s: status %d body %s", inst.ID, rec.Code, rec.Body.String())
		}
		var out checkinResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	sshResp := checkin(sshInst)
	if len(sshResp.Applications) != 1 || sshResp.Applications[0].InventoryData == "" {
		t.Fatalf("ssh checkin missing hub inventory: %+v", sshResp.Applications)
	}
	agentResp := checkin(agentInst)
	if len(agentResp.Applications) != 1 || agentResp.Applications[0].InventoryData != "" {
		t.Fatalf("agent checkin leaked hub inventory: %+v", agentResp.Applications)
	}
}
