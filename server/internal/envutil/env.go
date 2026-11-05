// Package envutil reads Helios settings from the environment while preserving
// compatibility with the pre-rename ANSIBLEHUB_*, ANH_* and AHUB_* names.
package envutil

import (
	"os"
	"strings"
)

var prefixes = [...]string{"HELIOS_", "ANSIBLEHUB_", "ANH_", "AHUB_"}

// Get returns the first non-empty value for name, trying HELIOS_* first and
// then each legacy prefix. The caller may pass either "NAME" or "HELIOS_NAME".
func Get(name string) string {
	name = strings.TrimPrefix(name, "HELIOS_")
	for _, prefix := range prefixes {
		if v := os.Getenv(prefix + name); v != "" {
			return v
		}
	}
	return ""
}

// Or returns Get(name), or def when the setting is unset.
func Or(name, def string) string {
	if v := Get(name); v != "" {
		return v
	}
	return def
}
