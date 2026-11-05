// Package metrics exposes a tiny Prometheus-text metrics registry. It avoids a
// client library dependency: Helios only needs a handful of counters.
package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type key struct {
	name   string
	labels string
}

var (
	mu       sync.Mutex
	counters = map[key]float64{}
	info     = map[string]string{}
)

func inc(name, labels string) {
	mu.Lock()
	counters[key{name, labels}]++
	mu.Unlock()
}

// Checkin counts successful agent checkins.
func Checkin(instance string) {
	inc("helios_agent_checkins_total", `instance="`+esc(instance)+`"`)
}

// Run counts reported runs.
func Run(instance string, appID int64, mode, status string) {
	inc("helios_runs_total", fmt.Sprintf(`instance="%s",app="%d",mode="%s",status="%s"`,
		esc(instance), appID, esc(mode), esc(status)))
}

// Command counts command results.
func Command(status string) {
	inc("helios_commands_total", `status="`+esc(status)+`"`)
}

// Frozen counts syncs blocked by maintenance windows.
func Frozen(appID int64) {
	inc("helios_sync_frozen_total", `app="`+strconv.FormatInt(appID, 10)+`"`)
}

// SetInfo publishes a build info gauge.
func SetInfo(version string) {
	mu.Lock()
	info["helios_build_info"] = version
	mu.Unlock()
}

// Handler serves the Prometheus text exposition format.
func Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		mu.Lock()
		keys := make([]key, 0, len(counters))
		for k := range counters {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].name != keys[j].name {
				return keys[i].name < keys[j].name
			}
			return keys[i].labels < keys[j].labels
		})
		var b strings.Builder
		for _, k := range keys {
			fmt.Fprintf(&b, "%s{%s} %g\n", k.name, k.labels, counters[k])
		}
		versions := make([]string, 0, len(info))
		for k := range info {
			versions = append(versions, k)
		}
		sort.Strings(versions)
		for _, k := range versions {
			fmt.Fprintf(&b, "%s{version=\"%s\"} 1\n", k, esc(info[k]))
		}
		mu.Unlock()
		_, _ = w.Write([]byte(b.String()))
	}
}

func esc(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return strings.ReplaceAll(v, "\n", `\n`)
}
