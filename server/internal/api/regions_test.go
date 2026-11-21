package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/slim/helios/server/internal/model"
)

func TestRegionEndpoints(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()

	create := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.handleCreateRegion(rec, httptest.NewRequest(http.MethodPost, "/api/v1/regions", strings.NewReader(body)))
		return rec
	}
	if rec := create(`{"name":" eu-west-1 ","description":" Paris "}`); rec.Code != http.StatusCreated {
		t.Fatalf("create region: status %d body %s", rec.Code, rec.Body.String())
	}
	if rec := create(`{"name":"EU-WEST-1"}`); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate region: status %d body %s, want 409", rec.Code, rec.Body.String())
	}
	if rec := create(`{"name":"  "}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("blank region: status %d body %s, want 400", rec.Code, rec.Body.String())
	}

	regions, err := st.ListRegions(ctx)
	if err != nil || len(regions) != 1 || regions[0].Name != "eu-west-1" || regions[0].Description != "Paris" {
		t.Fatalf("list regions: %v %+v", err, regions)
	}

	rec := httptest.NewRecorder()
	s.handleListRegions(rec, httptest.NewRequest(http.MethodGet, "/api/v1/regions", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"eu-west-1"`) {
		t.Fatalf("get regions: status %d body %s", rec.Code, rec.Body.String())
	}

	if _, err := st.CreateInstance(ctx, model.Instance{
		ID: "ins-1", Name: "one", TokenHash: "h1", Region: "eu-west-1",
	}); err != nil {
		t.Fatalf("create instance: %v", err)
	}
	update := `{"name":"eu-west-primary","description":"Paris primary"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/regions/1", strings.NewReader(update))
	req.SetPathValue("regionId", "1")
	rec = httptest.NewRecorder()
	s.handleUpdateRegion(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update region: status %d body %s", rec.Code, rec.Body.String())
	}
	inst, err := st.GetInstance(ctx, "ins-1")
	if err != nil || inst.Region != "eu-west-primary" {
		t.Fatalf("instance region after rename = %q err=%v", inst.Region, err)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/regions/1", nil)
	req.SetPathValue("regionId", "1")
	rec = httptest.NewRecorder()
	s.handleDeleteRegion(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete region: status %d body %s", rec.Code, rec.Body.String())
	}
	if _, err := st.GetRegionByID(ctx, 1); err == nil {
		t.Fatal("region still exists after delete")
	}
}
