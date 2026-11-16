package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/slim/helios/server/internal/model"
)

func TestBulkCommandsQueuesEveryAssignment(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	if _, err := st.CreateInstance(ctx, model.Instance{ID: "i1", Name: "host1", TokenHash: "h1"}); err != nil {
		t.Fatalf("create instance: %v", err)
	}
	app, err := st.CreateApplication(ctx, model.Application{
		Name: "demo", RepoURL: "http://repo/demo.git", Branch: "main", Path: "site.yml", Vars: "{}",
	})
	if err != nil {
		t.Fatalf("create app: %v", err)
	}
	if err := st.Assign(ctx, "i1", app.ID); err != nil {
		t.Fatalf("assign: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/bulk/commands", strings.NewReader(`{"action":"check","instance_ids":["i1","missing"]}`))
	rec := httptest.NewRecorder()
	s.handleBulkCommands(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk: status %d body %s", rec.Code, rec.Body.String())
	}
	var got bulkCommandResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Queued != 1 || len(got.Errors) != 1 {
		t.Fatalf("result = %+v, want 1 queued and 1 error", got)
	}
	cmds, err := st.PendingCommands(ctx, "i1")
	if err != nil {
		t.Fatalf("pending commands: %v", err)
	}
	if len(cmds) != 1 || cmds[0].Action != model.ActionCheck {
		t.Fatalf("commands = %+v", cmds)
	}
}
