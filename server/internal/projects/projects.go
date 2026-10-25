// Package projects validates project names and repository allowlist patterns.
package projects

import (
	"regexp"
	"strings"
)

// Name is the implicit global/default project (no repository restriction, no
// RBAC scope).
const Global = ""

// ValidName reports whether a project name is acceptable. Project names are
// used as RBAC object scopes, so keep them simple and comma-free.
func ValidName(name string) bool {
	if name == Global {
		return true
	}
	if len(name) > 63 || strings.TrimSpace(name) != name {
		return false
	}
	ok, _ := regexp.MatchString(`^[A-Za-z0-9][A-Za-z0-9._-]*$`, name)
	return ok
}

// MatchRepo reports whether url is allowed by the project's newline/comma
// separated glob patterns. An empty pattern list allows everything. "*"
// matches any sequence, including "/".
func MatchRepo(patterns, url string) bool {
	patterns = strings.TrimSpace(patterns)
	if patterns == "" {
		return true
	}
	fields := strings.FieldsFunc(patterns, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == ';'
	})
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		re := regexp.QuoteMeta(f)
		re = strings.ReplaceAll(re, `\*`, `.*`)
		re = strings.ReplaceAll(re, `\?`, `.`)
		if ok, _ := regexp.MatchString("(?i)^"+re+"$", url); ok {
			return true
		}
	}
	return false
}
