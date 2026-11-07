package api

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/slim/helios/server/internal/model"
)

func TestMergedVarsPrecedence(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()

	inst, err := st.CreateInstance(ctx, model.Instance{ID: "ins-1", Name: "host-1", TokenHash: "hash-1"})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	app, err := st.CreateApplication(ctx, model.Application{
		Name: "demo", RepoURL: "https://example.com/demo.git", Branch: "main", Path: "site.yml",
		Vars: `{"legacy":"yes","override":"legacy","legacy_num":1}`,
	})
	if err != nil {
		t.Fatalf("create app: %v", err)
	}
	if err := st.Assign(ctx, inst.ID, app.ID); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := st.SaveAppVariables(ctx, app.ID, []model.Variable{
		{Key: "env", Value: "prod"},
		{Key: "override", Value: "app"},
		{Key: "count", Value: "3"},
	}); err != nil {
		t.Fatalf("save app variables: %v", err)
	}
	if err := st.SaveAssignmentVariables(ctx, inst.ID, app.ID, []model.Variable{
		{Key: "env", Value: "prod-east"},
		{Key: "enabled", Value: "true"},
		{Key: "tags", Value: `["a","b"]`},
	}); err != nil {
		t.Fatalf("save assignment variables: %v", err)
	}

	raw, err := s.mergedVars(ctx, inst.ID, app.ID, app.Vars)
	if err != nil {
		t.Fatalf("merged vars: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode merged vars %q: %v", raw, err)
	}
	want := map[string]any{
		"legacy":     "yes",
		"legacy_num": float64(1),
		"override":   "app",
		"env":        "prod-east",
		"count":      float64(3),
		"enabled":    true,
		"tags":       []any{"a", "b"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged vars = %#v, want %#v", got, want)
	}
}
