package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/slim/helios/server/internal/rbac"
)

func TestRBACAdminAPILoadsAndReloadsFromDB(t *testing.T) {
	s, _ := newTestServer(t)
	s.opAuth = &OpAuth{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac", nil)
	rec := httptest.NewRecorder()
	s.handleGetRBAC(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
	var initial rbacResponse
	if err := json.NewDecoder(rec.Body).Decode(&initial); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(initial.Policies) != 0 || len(initial.Bindings) != 0 {
		t.Fatalf("expected empty db policy, got %+v", initial)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/rbac/policies",
		strings.NewReader(`{"role":"role:test","resource":"applications","action":"get","object":"acme"}`))
	rec = httptest.NewRecorder()
	s.handleCreateRBACPolicy(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create policy: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/rbac/bindings",
		strings.NewReader(`{"group":"acme-team","role":"role:test"}`))
	rec = httptest.NewRecorder()
	s.handleCreateRBACBinding(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create binding: %d %s", rec.Code, rec.Body.String())
	}

	rb := s.currentRBAC()
	if rb == nil {
		t.Fatal("currentRBAC is nil after db reload")
	}
	if !rb.Authorize([]string{"acme-team"}, nil, rbac.ResApplications, rbac.ActionGet, "acme") {
		t.Fatal("expected db-backed policy to authorize acme-team on acme applications")
	}
	if rb.Authorize([]string{"acme-team"}, nil, rbac.ResApplications, rbac.ActionGet, "other") {
		t.Fatal("policy leaked to another project scope")
	}
	if !rb.Authorize(nil, []string{"role:admin"}, rbac.ResRBAC, rbac.ActionUpdate, "*") {
		t.Fatal("built-in role:admin should still manage rbac")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/rbac", nil)
	rec = httptest.NewRecorder()
	s.handleGetRBAC(rec, req)
	var got rbacResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Source != "db" || len(got.Policies) != 1 || len(got.Bindings) != 1 {
		t.Fatalf("unexpected rbac response: %+v", got)
	}
}

func TestRBACDeleteLastAdminBindingIsBlockedWithoutBasic(t *testing.T) {
	s, _ := newTestServer(t)
	s.opAuth = &OpAuth{} // no Basic: OIDC-only deployment

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rbac/bindings",
		strings.NewReader(`{"group":"sre@example.com","role":"role:admin"}`))
	rec := httptest.NewRecorder()
	s.handleCreateRBACBinding(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create admin binding: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/rbac/bindings/"+strconv.FormatInt(created.ID, 10), nil)
	req.SetPathValue("id", strconv.FormatInt(created.ID, 10))
	rec = httptest.NewRecorder()
	s.handleDeleteRBACBinding(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("delete last admin binding: %d body %s", rec.Code, rec.Body.String())
	}
}
