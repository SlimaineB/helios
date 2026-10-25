package ansible

import "testing"

func TestFilterIgnored(t *testing.T) {
	res := Result{
		Status:  StatusDrift,
		Summary: map[string]int{"changed": 2, "ok": 3},
		ChangedTasks: []ChangedTask{
			{TaskName: "Install nginx", Host: "web1"},
			{TaskName: "Write /etc/motd", Host: "web1"},
		},
		Resources: []Resource{
			{Module: "copy", Target: "/etc/motd"},
			{Module: "ansible.builtin.service", Target: "nginx"},
		},
	}
	FilterIgnored(&res, "write /etc/*\n/etc/*\nansible.builtin.service nginx")
	if res.Status != StatusDrift {
		t.Fatalf("status = %q, want drift (one task remains)", res.Status)
	}
	if len(res.ChangedTasks) != 1 || res.ChangedTasks[0].TaskName != "Install nginx" {
		t.Fatalf("unexpected changed tasks: %+v", res.ChangedTasks)
	}
	if res.Summary["changed"] != 1 {
		t.Fatalf("summary changed = %d, want 1", res.Summary["changed"])
	}
	if len(res.Resources) != 0 {
		t.Fatalf("resources not filtered: %+v", res.Resources)
	}
}

func TestFilterIgnoredAllChangesBecomesOK(t *testing.T) {
	res := Result{
		Status:       StatusDrift,
		Summary:      map[string]int{"changed": 1},
		ChangedTasks: []ChangedTask{{TaskName: "tuning", Host: "db1"}},
	}
	FilterIgnored(&res, "tuning, prod-*")
	if res.Status != StatusOK {
		t.Fatalf("status = %q, want ok", res.Status)
	}
	if len(res.ChangedTasks) != 0 {
		t.Fatalf("changed tasks not filtered: %+v", res.ChangedTasks)
	}
}

func TestFilterIgnoredNoPatterns(t *testing.T) {
	res := Result{Status: StatusDrift, ChangedTasks: []ChangedTask{{TaskName: "x"}}}
	FilterIgnored(&res, "")
	if res.Status != StatusDrift || len(res.ChangedTasks) != 1 {
		t.Fatal("empty patterns must not change the result")
	}
}
