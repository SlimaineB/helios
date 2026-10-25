// Package rbac implements ArgoCD-style role-based access control with
// `p` (policy) and `g` (group membership) rules loaded from a YAML file.
//
// Built-in roles always exist:
//
//	role:admin    -> every action on every resource
//	role:readonly -> get on every resource
//
// The policy file adds rules. A request is allowed when at least one rule
// matching (role or group, resource, action, object) grants it; otherwise it is
// denied. The object is a project name, or "*" for every project. There is no
// deny-list.
//
//	# rbac.yaml
//	policies:
//	  - "p, role:viewer, applications, get, *"
//	  - "p, role:dev, applications, update, finance"   # finance project only
//	  - "g, Engineering, role:readonly"   # Okta group -> builtin role
//	  - "g, SRE, role:admin"
package rbac

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Resources and actions used by the operator API. Mirror of the routes.
const (
	ResApplications = "applications"
	ResInstances    = "instances"
	ResRepositories = "repositories"
	ResRuns         = "runs"
	ResCommands     = "commands"
	ResWindows      = "windows"
	ResAudit        = "audit"
	ResProjects     = "projects"
)

const (
	ActionGet    = "get"
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionDelete = "delete"
	ActionSync   = "sync" // Sync/Apply command
)

// AllResources is used by the built-in roles.
var AllResources = []string{ResApplications, ResInstances, ResRepositories, ResRuns, ResCommands, ResWindows, ResAudit, ResProjects}

// AllActions is used by the built-in admin role.
var AllActions = []string{ActionGet, ActionCreate, ActionUpdate, ActionDelete, ActionSync}

// builtinRules are merged under any custom rules and can never be removed.
var builtinRules = func() []rule {
	var out []rule
	for _, r := range AllResources {
		out = append(out, rule{Role: "role:admin", Resource: r, Action: "*", Object: "*"})
	}
	for _, r := range AllResources {
		out = append(out, rule{Role: "role:readonly", Resource: r, Action: ActionGet, Object: "*"})
	}
	return out
}()

type rule struct {
	Role     string // role name, optionally bound to groups via bindings
	Resource string
	Action   string
	Object   string // project name, or "*" for every project
}

type binding struct {
	Group string // group name from the OIDC claim
	Role  string
}

// RBAC is an immutable policy set built once at startup.
type RBAC struct {
	rules    []rule
	bindings []binding
}

// Config mirrors the YAML file shape:
//
//	policies: ["p, role:r, applications, get, *", ...]
//	bindings: ["g, grin, role:r", ...]
type Config struct {
	Policies []string `yaml:"policies"`
	Bindings []string `yaml:"bindings"`
}

// FromFile loads a policy file. An empty/missing file yields only the
// built-in roles.
func FromFile(path string) (*RBAC, error) {
	var cfg Config
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read rbac file: %w", err)
		}
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return nil, fmt.Errorf("parse rbac file: %w", err)
		}
	}
	return New(cfg)
}

// New builds the RBAC policy set from a Config.
func New(cfg Config) (*RBAC, error) {
	r := &RBAC{rules: append([]rule(nil), builtinRules...)}
	for _, line := range cfg.Policies {
		ru, err := parsePolicy(line)
		if err != nil {
			return nil, err
		}
		r.rules = append(r.rules, ru)
	}
	for _, line := range cfg.Bindings {
		b, err := parseBinding(line)
		if err != nil {
			return nil, err
		}
		r.bindings = append(r.bindings, b)
	}
	return r, nil
}

// parsePolicy handles "p, <rolename>, <resource>, <action>, <object>". The
// object defaults to "*" (all projects).
func parsePolicy(line string) (rule, error) {
	parts := splitTrim(line)
	if len(parts) < 4 || parts[0] != "p" {
		return rule{}, fmt.Errorf("bad policy line %q, want: p, role, resource, action[, object]", line)
	}
	object := "*"
	if len(parts) >= 5 {
		object = parts[4]
	}
	if object == "" {
		return rule{}, fmt.Errorf("empty object scope: %s", line)
	}
	return rule{Role: parts[1], Resource: parts[2], Action: parts[3], Object: object}, nil
}

// parseBinding handles "g, <group>, <role>".
func parseBinding(line string) (binding, error) {
	parts := splitTrim(line)
	if len(parts) != 3 || parts[0] != "g" {
		return binding{}, fmt.Errorf("bad binding line %q, want: g, group, role", line)
	}
	return binding{Group: parts[1], Role: parts[2]}, nil
}

func splitTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// Authorize reports whether a principal may perform action on resource within
// object (a project name, "" for global). A "*" rule matches every object; a
// project-scoped rule never grants access to the global scope.
func (r *RBAC) Authorize(groups, roles []string, resource, action, object string) bool {
	for _, rule := range r.rules {
		if rule.Resource != resource && rule.Resource != "*" {
			continue
		}
		if rule.Action != action && rule.Action != "*" {
			continue
		}
		if rule.Object != "*" && (object == "" || rule.Object != object) {
			continue
		}
		if matchIdentity(rule.Role, roles, groups, r.bindings) {
			return true
		}
	}
	return false
}

// AuthorizeAny reports whether the principal has the operation on at least one
// object. It is the coarse gate used before handlers resolve a concrete scope.
func (r *RBAC) AuthorizeAny(groups, roles []string, resource, action string) bool {
	for _, rule := range r.rules {
		if rule.Resource != resource && rule.Resource != "*" {
			continue
		}
		if rule.Action != action && rule.Action != "*" {
			continue
		}
		if matchIdentity(rule.Role, roles, groups, r.bindings) {
			return true
		}
	}
	return false
}

// VisibleObjects returns every project the principal can act on for
// resource.action. all is true when a "*" rule grants global visibility.
func (r *RBAC) VisibleObjects(groups, roles []string, resource, action string) (all bool, objects []string) {
	seen := map[string]bool{}
	for _, rule := range r.rules {
		if rule.Resource != resource && rule.Resource != "*" {
			continue
		}
		if rule.Action != action && rule.Action != "*" {
			continue
		}
		if !matchIdentity(rule.Role, roles, groups, r.bindings) {
			continue
		}
		if rule.Object == "*" {
			return true, nil
		}
		if !seen[rule.Object] {
			seen[rule.Object] = true
			objects = append(objects, rule.Object)
		}
	}
	return false, objects
}

// matchIdentity reports whether the rule's identity (a role or group) matches
// the principal's roles or groups.
func matchIdentity(identity string, roles, groups []string, bindings []binding) bool {
	for _, r := range roles {
		if r == identity {
			return true
		}
	}
	for _, g := range groups {
		if g == identity {
			return true
		}
	}
	for _, b := range bindings {
		if b.Role != identity {
			continue
		}
		for _, g := range groups {
			if g == b.Group {
				return true
			}
		}
	}
	return false
}

// RolesForGroups resolves the role names a set of groups is bound to.
func (r *RBAC) RolesForGroups(groups []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, b := range r.bindings {
		if seen[b.Role] {
			continue
		}
		for _, g := range groups {
			if g == b.Group {
				seen[b.Role] = true
				out = append(out, b.Role)
				break
			}
		}
	}
	return out
}

// Permissions returns the sorted allowed "resource.action" pairs for a
// principal — used by the UI to enable/disable actions and by /me.
func (r *RBAC) Permissions(groups, roles []string) []string {
	set := map[string]bool{}
	for _, res := range AllResources {
		for _, act := range AllActions {
			if r.AuthorizeAny(groups, roles, res, act) {
				set[res+"."+act] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for _, res := range AllResources {
		for _, act := range AllActions {
			if set[res+"."+act] {
				out = append(out, res+"."+act)
			}
		}
	}
	return out
}
