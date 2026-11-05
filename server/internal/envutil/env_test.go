package envutil

import "testing"

func TestGetFallsBackToLegacyNames(t *testing.T) {
	t.Setenv("HELIOS_DB", "")
	t.Setenv("ANSIBLEHUB_DB", "postgres://legacy")
	t.Setenv("ANH_OPERATOR_USER", "legacy-admin")
	t.Setenv("AHUB_OPERATOR_PASSWORD", "legacy-pass")

	if got := Get("DB"); got != "postgres://legacy" {
		t.Fatalf("Get(DB) = %q, want legacy fallback", got)
	}
	if got := Get("OPERATOR_USER"); got != "legacy-admin" {
		t.Fatalf("Get(OPERATOR_USER) = %q, want ANH fallback", got)
	}
	if got := Get("OPERATOR_PASSWORD"); got != "legacy-pass" {
		t.Fatalf("Get(OPERATOR_PASSWORD) = %q, want AHUB fallback", got)
	}
}

func TestGetPrefersHeliosName(t *testing.T) {
	t.Setenv("HELIOS_DB", "postgres://new")
	t.Setenv("ANSIBLEHUB_DB", "postgres://legacy")

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
