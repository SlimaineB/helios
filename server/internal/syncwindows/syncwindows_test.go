package syncwindows

import (
	"testing"
	"time"

	"github.com/slim/helios/server/internal/model"
)

func at(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestDenyWindowFreezesWhileActive(t *testing.T) {
	w := model.SyncWindow{Kind: "deny", Enabled: true, Timezone: "UTC", Days: "mon", StartMin: 9 * 60, EndMin: 11 * 60}
	// 2026-10-26 is a Monday.
	if !Frozen([]model.SyncWindow{w}, 1, at(t, "2026-10-26T10:00:00Z")) {
		t.Fatal("expected frozen inside deny window")
	}
	if Frozen([]model.SyncWindow{w}, 1, at(t, "2026-10-26T12:00:00Z")) {
		t.Fatal("expected open after deny window")
	}
	if Frozen([]model.SyncWindow{w}, 1, at(t, "2026-10-27T10:00:00Z")) {
		t.Fatal("expected open on another day")
	}
}

func TestAllowWindowFreezesOutside(t *testing.T) {
	w := model.SyncWindow{Kind: "allow", Enabled: true, Timezone: "UTC", Days: "mon", StartMin: 9 * 60, EndMin: 11 * 60}
	if !Frozen([]model.SyncWindow{w}, 1, at(t, "2026-10-26T12:00:00Z")) {
		t.Fatal("expected frozen outside allow window")
	}
	if Frozen([]model.SyncWindow{w}, 1, at(t, "2026-10-26T10:00:00Z")) {
		t.Fatal("expected open inside allow window")
	}
}

func TestOvernightAndTimezone(t *testing.T) {
	w := model.SyncWindow{Kind: "deny", Enabled: true, Timezone: "Europe/Paris", Days: "mon", StartMin: 23 * 60, EndMin: 2 * 60}
	// 2026-10-26 00:30 Paris == 2026-10-25 22:30 UTC (Sunday UTC, Monday Paris).
	if !Frozen([]model.SyncWindow{w}, 1, at(t, "2026-10-25T23:30:00Z")) {
		t.Fatal("expected frozen after midnight in window timezone")
	}
	if Frozen([]model.SyncWindow{w}, 1, at(t, "2026-10-26T12:00:00Z")) {
		t.Fatal("expected open during the day")
	}
}

func TestScopedAndDisabled(t *testing.T) {
	w := model.SyncWindow{Kind: "deny", Enabled: true, AppID: 7, Timezone: "UTC"}
	if Frozen([]model.SyncWindow{w}, 8, at(t, "2026-10-26T10:00:00Z")) {
		t.Fatal("app-scoped window must not affect another app")
	}
	w.Enabled = false
	if Frozen([]model.SyncWindow{w}, 7, at(t, "2026-10-26T10:00:00Z")) {
		t.Fatal("disabled window must not freeze")
	}
}

func TestValidateAndHM(t *testing.T) {
	good, msg := Validate(model.SyncWindow{Name: "maint", Kind: "DENY", Days: "Mon, tue", Timezone: "UTC"})
	if msg != "" || good.Kind != "deny" || good.Days != "mon,tue" {
		t.Fatalf("validate = %+v %q", good, msg)
	}
	if _, msg := Validate(model.SyncWindow{Name: "x", Kind: "nope"}); msg == "" {
		t.Fatal("expected invalid kind")
	}
	if m, ok := ParseHM("09:30"); !ok || m != 570 || FormatHM(m) != "09:30" {
		t.Fatalf("HM roundtrip failed: %d %v %q", m, ok, FormatHM(m))
	}
}
