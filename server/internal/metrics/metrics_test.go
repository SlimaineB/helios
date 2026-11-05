package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExposition(t *testing.T) {
	Run("ins-1", 7, "apply", "failed")
	Command("failed")
	Frozen(7)
	rec := httptest.NewRecorder()
	Handler()(rec, httptest.NewRequest("GET", "/metrics", nil))
	out := rec.Body.String()
	for _, want := range []string{
		`helios_runs_total{instance="ins-1",app="7",mode="apply",status="failed"}`,
		`helios_commands_total{status="failed"}`,
		`helios_sync_frozen_total{app="7"}`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("metrics output missing %q:\n%s", want, out)
		}
	}
}
