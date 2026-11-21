package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGlobalSyncLockRequiresReasonAndFreezes(t *testing.T) {
	s, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/sync-lock", strings.NewReader(`{"enabled":true}`))
	rec := httptest.NewRecorder()
	s.handleSetSyncLock(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("lock without reason: %d body %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/api/v1/sync-lock", strings.NewReader(`{"enabled":true,"reason":"prod incident INC-1234"}`))
	rec = httptest.NewRecorder()
	s.handleSetSyncLock(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("lock: %d body %s", rec.Code, rec.Body.String())
	}
	var lock syncLock
	if err := json.NewDecoder(rec.Body).Decode(&lock); err != nil {
		t.Fatalf("decode lock: %v", err)
	}
	if !lock.Enabled || lock.Reason != "prod incident INC-1234" {
		t.Fatalf("lock = %+v", lock)
	}
	if !s.frozen(context.Background(), 42) {
		t.Fatal("global sync lock should freeze every app")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/sync-lock", nil)
	rec = httptest.NewRecorder()
	s.handleGetSyncLock(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get lock: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/v1/sync-lock", strings.NewReader(`{"enabled":false}`))
	rec = httptest.NewRecorder()
	s.handleSetSyncLock(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unlock: %d body %s", rec.Code, rec.Body.String())
	}
	if s.frozen(context.Background(), 42) {
		t.Fatal("after unlock, frozen should be false without windows")
	}
}
