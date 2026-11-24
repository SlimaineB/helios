package inventory

import "testing"

func findGroup(t *testing.T, r Result, name string) Group {
	t.Helper()
	for _, g := range r.Groups {
		if g.Name == name {
			return g
		}
	}
	t.Fatalf("group %q not found in %+v", name, r.Groups)
	return Group{}
}

func findHost(t *testing.T, g Group, name string) Host {
	t.Helper()
	for _, h := range g.Hosts {
		if h.Name == name {
			return h
		}
	}
	t.Fatalf("host %q not found in group %q", name, g.Name)
	return Host{}
}

func TestParseINI(t *testing.T) {
	r := Parse(map[string][]byte{
		"inventory/hosts.ini": []byte(`
[web]
web-1 ansible_host=10.0.0.11 ansible_user=deploy
web-2

[web:vars]
ansible_port=2222

[db]
db-1 ansible_host="10.0.0.21"

[all:children]
web
db
`),
	})
	if len(r.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", r.Warnings)
	}
	web := findGroup(t, r, "web")
	if got := findHost(t, web, "web-1").Vars["ansible_host"]; got != "10.0.0.11" {
		t.Fatalf("ansible_host = %v", got)
	}
	if got := web.Vars["ansible_port"]; got != "2222" {
		t.Fatalf("group var = %v", got)
	}
	if len(web.Hosts) != 2 {
		t.Fatalf("web hosts = %d", len(web.Hosts))
	}
	all := findGroup(t, r, "all")
	if len(all.Children) != 2 {
		t.Fatalf("all children = %v", all.Children)
	}
	db := findGroup(t, r, "db")
	if got := findHost(t, db, "db-1").Vars["ansible_host"]; got != "10.0.0.21" {
		t.Fatalf("quoted host var = %v", got)
	}
}

func TestParseYAMLAndMerge(t *testing.T) {
	r := Parse(map[string][]byte{
		"inventory/hosts.yml": []byte(`
all:
  children:
    web:
      hosts:
        web-1:
          ansible_host: 10.0.0.11
        web-2:
      vars:
        ansible_user: deploy
    db:
      hosts:
        db-1:
`),
		"inventory/extra.ini": []byte(`
[web]
web-3 ansible_host=10.0.0.13
`),
		"inventory/group_vars/web.yml": []byte(`ansible_user: ignored`),
	})
	if len(r.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", r.Warnings)
	}
	web := findGroup(t, r, "web")
	if len(web.Hosts) != 3 {
		t.Fatalf("web hosts = %v", web.Hosts)
	}
	if got := web.Vars["ansible_user"]; got != "deploy" {
		t.Fatalf("group var = %v", got)
	}
	if got := findHost(t, web, "web-1").Vars["ansible_host"]; got != "10.0.0.11" {
		t.Fatalf("host var = %v", got)
	}
}

func TestParseWarnsOnBrokenFile(t *testing.T) {
	r := Parse(map[string][]byte{
		"inventory/bad.ini":  []byte("host-without-section ansible_host=1.2.3.4\n"),
		"inventory/good.ini": []byte("[web]\nweb-1\n"),
	})
	if len(r.Warnings) != 1 {
		t.Fatalf("warnings = %v", r.Warnings)
	}
	if len(r.Groups) != 1 || r.Groups[0].Name != "web" {
		t.Fatalf("groups = %+v", r.Groups)
	}
}
