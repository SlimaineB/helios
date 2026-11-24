package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/slim/helios/server/internal/model"
)

func leaseRequest(method, instanceID, runner string) *http.Request {
	req := httptest.NewRequest(method, "/api/v1/runner/instances/"+instanceID+"/lease", nil)
	req.SetPathValue("id", instanceID)
	if runner != "" {
		req.Header.Set(runnerIDHeader, runner)
	}
	return req
}

func TestRunnerInstanceLeaseExclusive(t *testing.T) {
	s, st := newTestServer(t)
	inst := createInstance(t, st, "i1", model.ModeSSH, "web-1")

	rec := httptest.NewRecorder()
	s.handleRunnerClaim(rec, leaseRequest(http.MethodPost, inst.ID, "runner-a"))
	if rec.Code != http.StatusOK {
		t.Fatalf("claim a: status %d body %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	s.handleRunnerClaim(rec, leaseRequest(http.MethodPost, inst.ID, "runner-b"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("claim b: status %d body %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	s.handleRunnerRenew(rec, leaseRequest(http.MethodPut, inst.ID, "runner-a"))
	if rec.Code != http.StatusOK {
		t.Fatalf("renew a: status %d body %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	s.handleRunnerRenew(rec, leaseRequest(http.MethodPut, inst.ID, "runner-b"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("renew b: status %d body %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	s.handleRunnerRelease(rec, leaseRequest(http.MethodDelete, inst.ID, "runner-a"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("release a: status %d body %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	s.handleRunnerClaim(rec, leaseRequest(http.MethodPost, inst.ID, "runner-b"))
	if rec.Code != http.StatusOK {
		t.Fatalf("claim b after release: status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestRunnerLeaseRequiresRunnerID(t *testing.T) {
	s, st := newTestServer(t)
	inst := createInstance(t, st, "i1", model.ModeSSH, "web-1")

	rec := httptest.NewRecorder()
	s.handleRunnerClaim(rec, leaseRequest(http.MethodPost, inst.ID, ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestRunnerLeaseRejectsAgentInstance(t *testing.T) {
	s, st := newTestServer(t)
	inst := createInstance(t, st, "i1", model.ModeAgent, "")

	rec := httptest.NewRecorder()
	s.handleRunnerClaim(rec, leaseRequest(http.MethodPost, inst.ID, "runner-a"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}
