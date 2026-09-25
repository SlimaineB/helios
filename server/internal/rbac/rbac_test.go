package rbac

import (
	"strings"
	"testing"
)

func mustNew(t *testing.T, cfg Config) *RBAC {
	t.Helper()
	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

func TestBuiltinRoles(t *testing.T) {
	r := mustNew(t, Config{})
	if !r.Authorize(nil, []string{"role:admin"}, ResApplications, ActionDelete) {
		t.Error("admin should delete applications")
	}
	if !r.Authorize(nil, []string{"role:admin"}, ResCommands, ActionSync) {
		t.Error("admin should sync")
	}
	if !r.Authorize(nil, []string{"role:readonly"}, ResInstances, ActionGet) {
		t.Error("readonly should get instances")
	}
	if r.Authorize(nil, []string{"role:readonly"}, ResInstances, ActionDelete) {
		t.Error("readonly must NOT delete instances")
	}
	if r.Authorize(nil, []string{"role:readonly"}, ResCommands, ActionSync) {
		t.Error("readonly must NOT sync")
	}
	if r.Authorize(nil, nil, ResInstances, ActionGet) {
		t.Error("anonymous must get nothing")
	}
}

func TestGroupBindingsToBuiltin(t *testing.T) {
	r := mustNew(t, Config{Bindings: []string{
		"g, Engineering, role:readonly",
		"g, SRE-Platform, role:admin",
	}})
	if !r.Authorize([]string{"Engineering"}, nil, ResRuns, ActionGet) {
		t.Error("Engineering group should read runs")
	}
	if r.Authorize([]string{"Engineering"}, nil, ResApplications, ActionUpdate) {
		t.Error("Engineering group must not update applications")
	}
	if !r.Authorize([]string{"SRE-Platform"}, nil, ResCommands, ActionSync) {
		t.Error("SRE-Platform group should sync")
	}
	if r.Authorize([]string{"Security"}, nil, ResInstances, ActionGet) {
		t.Error("unbound group must have no access")
	}
}

func TestCustomPolicyRoleWithActions(t *testing.T) {
	r := mustNew(t, Config{
		Policies: []string{
			"p, role:deploy, commands, sync, *",
			"p, role:deploy, applications, get, *",
		},
		Bindings: []string{"g, Release-Eng, role:deploy"},
	})
	if !r.Authorize([]string{"Release-Eng"}, nil, ResCommands, ActionSync) {
		t.Error("Release-Eng should sync")
	}
	if !r.Authorize([]string{"Release-Eng"}, nil, ResApplications, ActionGet) {
		t.Error("Release-Eng should read applications")
	}
	if r.Authorize([]string{"Release-Eng"}, nil, ResApplications, ActionUpdate) {
		t.Error("Release-Eng must not update applications")
	}
}

func TestCustomGroupReviewedAsPolicyIdentity(t *testing.T) {
	// A policy may reference a group directly.
	r := mustNew(t, Config{
		Policies: []string{"p, Sensitive-Ops, instances, get, *"},
	})
	if !r.Authorize([]string{"Sensitive-Ops"}, nil, ResInstances, ActionGet) {
		t.Error("direct group policy should grant get")
	}
	if r.Authorize([]string{"Sensitive-Ops"}, nil, ResInstances, ActionCreate) {
		t.Error("direct group policy must not grant create")
	}
}

func TestPermissions(t *testing.T) {
	r := mustNew(t, Config{Bindings: []string{"g, Viewers, role:readonly"}})
	perms := r.Permissions([]string{"Viewers"}, nil)
	if len(perms) != len(AllResources) {
		t.Fatalf("readonly should hold exactly one get per resource, got %v", perms)
	}
	for _, p := range perms {
		if !strings.HasSuffix(p, ".get") {
			t.Errorf("readonly must only get: %s", p)
		}
	}
}

func TestBadLines(t *testing.T) {
	if _, err := New(Config{Policies: []string{"p, role:x, apps, get, /specific/app"}}); err == nil {
		t.Error("object scope should be rejected")
	}
	if _, err := New(Config{Policies: []string{"junk, line"}}); err == nil {
		t.Error("malformed policy should be rejected")
	}
	if _, err := New(Config{Bindings: []string{"g, onlytwo, parts, here"}}); err == nil {
		t.Error("malformed binding should be rejected")
	}
}
