// Package inventory parses the static Ansible inventories stored in an
// application repository. It supports INI files, YAML files and directories
// containing either, which covers the formats the runner can execute against.
package inventory

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Host is one inventory host and its connection variables.
type Host struct {
	Name string         `json:"name"`
	Vars map[string]any `json:"vars,omitempty"`
}

// Group is one inventory group. Children lists nested group names.
type Group struct {
	Name     string         `json:"name"`
	Vars     map[string]any `json:"vars,omitempty"`
	Children []string       `json:"children,omitempty"`
	Hosts    []Host         `json:"hosts"`
}

// Result is the merged inventory plus non-fatal parsing warnings.
type Result struct {
	Groups   []Group  `json:"groups"`
	Warnings []string `json:"warnings"`
}

// Parse merges every file in files. Keys are repo-relative paths, used to pick
// the format from the extension (YAML for .yml/.yaml, INI otherwise) and to
// report warnings. group_vars and host_vars directories are ignored.
func Parse(files map[string][]byte) Result {
	merged := map[string]*Group{}
	var warnings []string

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		clean := strings.TrimPrefix(strings.ReplaceAll(name, "\\", "/"), "./")
		if isVarsPath(clean) {
			continue
		}
		data := files[name]
		var (
			groups []Group
			err    error
		)
		if isYAML(clean) {
			groups, err = parseYAML(data)
		} else {
			groups, err = parseINI(data)
		}
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", clean, err))
			continue
		}
		merge(merged, groups)
	}

	out := Result{Warnings: warnings}
	out.Groups = make([]Group, 0, len(merged))
	for _, g := range merged {
		sort.Strings(g.Children)
		sort.Slice(g.Hosts, func(i, j int) bool { return g.Hosts[i].Name < g.Hosts[j].Name })
		out.Groups = append(out.Groups, *g)
	}
	sort.Slice(out.Groups, func(i, j int) bool { return out.Groups[i].Name < out.Groups[j].Name })
	return out
}

func isYAML(name string) bool {
	return strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")
}

func isVarsPath(name string) bool {
	for _, part := range strings.Split(name, "/") {
		if part == "group_vars" || part == "host_vars" {
			return true
		}
	}
	return false
}

func merge(dst map[string]*Group, groups []Group) {
	for _, g := range groups {
		cur, ok := dst[g.Name]
		if !ok {
			cur = &Group{Name: g.Name}
			dst[g.Name] = cur
		}
		if cur.Vars == nil && len(g.Vars) > 0 {
			cur.Vars = map[string]any{}
		}
		for k, v := range g.Vars {
			cur.Vars[k] = v
		}
		cur.Children = appendUnique(cur.Children, g.Children...)
		for _, h := range g.Hosts {
			cur.Hosts = upsertHost(cur.Hosts, h)
		}
	}
}

func upsertHost(hosts []Host, h Host) []Host {
	for i := range hosts {
		if hosts[i].Name != h.Name {
			continue
		}
		if hosts[i].Vars == nil && len(h.Vars) > 0 {
			hosts[i].Vars = map[string]any{}
		}
		for k, v := range h.Vars {
			hosts[i].Vars[k] = v
		}
		return hosts
	}
	return append(hosts, h)
}

func appendUnique(dst []string, values ...string) []string {
	for _, v := range values {
		found := false
		for _, existing := range dst {
			if existing == v {
				found = true
				break
			}
		}
		if !found {
			dst = append(dst, v)
		}
	}
	return dst
}

func parseINI(data []byte) ([]Group, error) {
	var (
		groups []Group
		index  map[string]int
		cur    = -1
		mode   = ""
	)
	index = map[string]int{}
	ensure := func(name string) *Group {
		name = strings.TrimSpace(name)
		if i, ok := index[name]; ok {
			return &groups[i]
		}
		groups = append(groups, Group{Name: name})
		index[name] = len(groups) - 1
		return &groups[len(groups)-1]
	}
	// Section names are group names; a suffix selects how lines are read.
	// Pointers into the slice are not stable across append, so resolve by name
	// on every line instead of holding a pointer.

	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := strings.TrimSpace(line[1 : len(line)-1])
			mode = ""
			switch {
			case strings.HasSuffix(section, ":vars"):
				section = strings.TrimSpace(strings.TrimSuffix(section, ":vars"))
				mode = "vars"
			case strings.HasSuffix(section, ":children"):
				section = strings.TrimSpace(strings.TrimSuffix(section, ":children"))
				mode = "children"
			}
			if section == "" {
				return nil, fmt.Errorf("empty section name")
			}
			ensure(section)
			cur = index[section]
			continue
		}
		if cur < 0 {
			return nil, fmt.Errorf("line outside a section: %q", line)
		}
		g := &groups[cur]
		switch mode {
		case "vars":
			k, v, ok := splitVar(line)
			if !ok {
				return nil, fmt.Errorf("invalid variable line: %q", line)
			}
			if g.Vars == nil {
				g.Vars = map[string]any{}
			}
			g.Vars[k] = v
		case "children":
			g.Children = appendUnique(g.Children, line)
		default:
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			host := Host{Name: fields[0]}
			for _, f := range fields[1:] {
				k, v, ok := splitVar(f)
				if !ok {
					return nil, fmt.Errorf("invalid host variable %q", f)
				}
				if host.Vars == nil {
					host.Vars = map[string]any{}
				}
				host.Vars[k] = v
			}
			g.Hosts = upsertHost(g.Hosts, host)
		}
	}
	return groups, nil
}

func splitVar(s string) (string, string, bool) {
	k, v, ok := strings.Cut(s, "=")
	if !ok || k == "" {
		return "", "", false
	}
	return strings.TrimSpace(k), unquote(strings.TrimSpace(v)), true
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func parseYAML(data []byte) ([]Group, error) {
	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	var groups []Group
	var walk func(name string, node any) error
	walk = func(name string, node any) error {
		g := Group{Name: name}
		body, _ := node.(map[string]any)
		if vars, ok := body["vars"].(map[string]any); ok {
			g.Vars = vars
		}
		if hosts, ok := body["hosts"].(map[string]any); ok {
			for hostName, hostVars := range hosts {
				h := Host{Name: hostName}
				if vars, ok := hostVars.(map[string]any); ok {
					h.Vars = vars
				}
				g.Hosts = append(g.Hosts, h)
			}
		}
		if children, ok := body["children"].(map[string]any); ok {
			for childName, childNode := range children {
				g.Children = appendUnique(g.Children, childName)
				if err := walk(childName, childNode); err != nil {
					return err
				}
			}
		}
		groups = append(groups, g)
		return nil
	}
	for name, node := range root {
		if err := walk(name, node); err != nil {
			return nil, err
		}
	}
	return groups, nil
}
