// Package envutil reads Helios settings from HELIOS_* environment variables.
package envutil

import (
	"os"
	"strings"
)

// Get returns the non-empty value of HELIOS_<name>. The caller may pass either
// "NAME" or "HELIOS_NAME".
func Get(name string) string {
	return os.Getenv("HELIOS_" + strings.TrimPrefix(name, "HELIOS_"))
}

// Or returns Get(name), or def when the setting is unset.
func Or(name, def string) string {
	if v := Get(name); v != "" {
		return v
	}
	return def
}
