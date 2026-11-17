package envutil

import "testing"

func TestGetReadsHeliosName(t *testing.T) {
	t.Setenv("HELIOS_DB", "postgres://new")

	if got := Get("DB"); got != "postgres://new" {
		t.Fatalf("Get(DB) = %q, want HELIOS_ value", got)
	}
	if got := Get("HELIOS_DB"); got != "postgres://new" {
		t.Fatalf("Get(HELIOS_DB) = %q, want HELIOS_ value", got)
	}
}

func TestOrUsesDefault(t *testing.T) {
	t.Setenv("HELIOS_MISSING", "")

	if got := Or("MISSING", "fallback"); got != "fallback" {
		t.Fatalf("Or(MISSING) = %q, want fallback", got)
	}
}
