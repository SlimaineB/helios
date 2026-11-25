package inventory

import (
	"strings"
	"testing"
)

func TestRenderINI(t *testing.T) {
	r := Result{Groups: []Group{
		{
			Name:     "web",
			Vars:     map[string]any{"ansible_user": "deploy"},
			Children: []string{"base"},
			Hosts: []Host{
				{Name: "web-1", Vars: map[string]any{"ansible_host": "10.0.0.11"}},
				{Name: "web-2", Vars: map[string]any{"ansible_host": "10.0.0.12", "ansible_port": 2222}},
			},
		},
		{Name: "base"},
	}}
	out := RenderINI(r)
	for _, want := range []string{
		"[web]\n",
		"web-1 ansible_host=10.0.0.11\n",
		"web-2 ansible_host=10.0.0.12 ansible_port=2222\n",
		"[web:vars]\nansible_user=deploy\n",
		"[web:children]\nbase\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered inventory missing %q:\n%s", want, out)
		}
	}
}

func TestParse(t *testing.T) {
	r, err := Parse(`{"groups":[{"name":"web","hosts":[{"name":"web-1"}]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Groups) != 1 || r.Groups[0].Hosts[0].Name != "web-1" {
		t.Fatalf("unexpected parse: %+v", r)
	}
	if _, err := Parse(""); err == nil {
		t.Fatal("empty inventory should fail")
	}
}
