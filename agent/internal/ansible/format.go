package ansible

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type hostResult struct {
	Changed     bool            `json:"changed"`
	Failed      bool            `json:"failed"`
	Ok          bool            `json:"ok"`
	Skipped     bool            `json:"skipped"`
	Unreachable bool            `json:"unreachable"`
	Msg         string          `json:"msg"`
	Action      string          `json:"action"`
	Invocation  json.RawMessage `json:"invocation"`
	Diff        json.RawMessage `json:"diff"`
	// Results holds one entry per loop iteration (the JSON stdout callback
	// reports loop tasks as a single host result with the items nested).
	Results []itemResult `json:"results"`
}

// itemResult is one iteration of a looped task. Skipped/failed iterations
// carry no invocation or diff.
type itemResult struct {
	Changed    bool            `json:"changed"`
	Failed     bool            `json:"failed"`
	Skipped    bool            `json:"skipped"`
	Invocation json.RawMessage `json:"invocation"`
	Diff       json.RawMessage `json:"diff"`
}

type statLine struct {
	Changed     int `json:"changed"`
	Failures    int `json:"failures"`
	Ok          int `json:"ok"`
	Skipped     int `json:"skipped"`
	Rescued     int `json:"rescued"`
	Ignored     int `json:"ignored"`
	Unreachable int `json:"unreachable"`
}

// Resource is one deployed/managed endpoint of a run: the module that acted on
// it and the primary target (dest/path/name/…).
type Resource struct {
	Module string `json:"module"`
	Target string `json:"target"`
}

// FormatText renders a readable, "ansible default callback"-style console log
// from the parsed JSON callback document. It powers the default log view in
// the UI; the raw JSON stays available via the toggle.
func FormatText(doc *callbackDoc) string {
	if doc == nil || len(doc.Plays) == 0 {
		return ""
	}
	var b strings.Builder
	for _, play := range doc.Plays {
		name := strings.TrimSpace(play.Play.Name)
		if name == "" {
			name = "PLAY"
		}
		b.WriteString("PLAY " + rule(name) + "\n\n")
		for _, t := range play.Tasks {
			tname := strings.TrimSpace(t.Task.Name)
			b.WriteString("TASK " + rule(tname) + "\n")
			for _, hr := range sortedHosts(t.Hosts) {
				b.WriteString(fmt.Sprintf("%s: [%s]\n", hr.result, hr.host))
				if hr.failed && hr.msg != "" {
					b.WriteString(fmt.Sprintf("fatal: [%s]: FAILED!\n%s\n", hr.host, hr.msg))
				}
			}
			b.WriteString("\n")
		}
	}

	// PLAY RECAP
	b.WriteString("PLAY RECAP " + strings.Repeat("*", 40) + "\n")
	for _, host := range sortedStatHosts(doc.Stats) {
		st := doc.Stats[host]
		b.WriteString(fmt.Sprintf("%-24s : ok=%d   changed=%d   failed=%d   skipped=%d   rescued=%d   ignored=%d\n",
			host, st.Ok, st.Changed, st.Failures, st.Skipped, st.Rescued, st.Ignored))
	}
	return b.String()
}

// FormatDiff renders only the diff hunks of the run (what a --check --diff
// would change). Used by the "Diff before apply" view.
func FormatDiff(doc *callbackDoc) string {
	if doc == nil {
		return ""
	}
	var b strings.Builder
	for _, play := range doc.Plays {
		for _, t := range play.Tasks {
			for hostKey, h := range t.Hosts {
				var rows []string
				for _, hk := range h.diffHunks() {
					rows = append(rows, renderHunk(hk)...)
				}
				if len(rows) == 0 {
					continue
				}
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				b.WriteString("--- " + t.Task.Name + " [" + hostKey + "]\n")
				for _, l := range rows {
					b.WriteString(l + "\n")
				}
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// diffHunks returns every diff hunk a task reported for a host, covering both
// plain task results and per-item loop results.
func (h *hostResult) diffHunks() []diffHunk {
	var hunks []diffHunk
	hunks = append(hunks, parseDiffs(h.Diff)...)
	for _, it := range h.Results {
		if it.Skipped || it.Failed {
			continue
		}
		hunks = append(hunks, parseDiffs(it.Diff)...)
	}
	return hunks
}

// targets returns the distinct endpoints a task touches on a host. Loops are
// expanded into one target per active iteration (each has its own invocation).
func (h *hostResult) targets() []string {
	if len(h.Results) > 0 {
		var out []string
		for _, it := range h.Results {
			if it.Skipped || it.Failed {
				continue
			}
			if tgt := moduleTarget(it.Invocation); tgt != "" {
				out = append(out, tgt)
			}
		}
		return out
	}
	if tgt := moduleTarget(h.Invocation); tgt != "" {
		return []string{tgt}
	}
	return nil
}

// extractResources lists the distinct (module, target) pairs a run touches.
func extractResources(doc *callbackDoc) []Resource {
	if doc == nil {
		return nil
	}
	var out []Resource
	seen := map[string]bool{}
	for _, play := range doc.Plays {
		for _, t := range play.Tasks {
			for _, h := range t.Hosts {
				mod := trimModule(h.Action)
				if mod == "" {
					continue
				}
				for _, tgt := range h.targets() {
					key := mod + "\x00" + tgt
					if seen[key] {
						continue
					}
					seen[key] = true
					out = append(out, Resource{Module: mod, Target: tgt})
				}
			}
		}
	}
	return out
}

func trimModule(action string) string {
	return strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(action), "ansible.builtin."), "ansible.")
}

func moduleTarget(inv json.RawMessage) string {
	if len(inv) == 0 {
		return ""
	}
	var parsed struct {
		ModuleArgs map[string]json.RawMessage `json:"module_args"`
	}
	if err := json.Unmarshal(inv, &parsed); err != nil {
		return ""
	}
	for _, k := range []string{"dest", "path", "name", "url", "key", "repo", "gid", "group", "user"} {
		if raw, ok := parsed.ModuleArgs[k]; ok {
			if s := scalarVal(raw, ""); s != "" {
				return s
			}
		}
	}
	return ""
}

type diffHunk struct {
	Before       json.RawMessage `json:"before"`
	After        json.RawMessage `json:"after"`
	BeforeHeader string          `json:"before_header"`
	AfterHeader  string          `json:"after_header"`
}

func parseDiffs(raw json.RawMessage) []diffHunk {
	if len(raw) == 0 {
		return nil
	}
	var many []diffHunk
	if err := json.Unmarshal(raw, &many); err == nil {
		return many
	}
	var one diffHunk
	if err := json.Unmarshal(raw, &one); err == nil {
		return []diffHunk{one}
	}
	return nil
}

func renderHunk(hk diffHunk) []string {
	before := scalarVal(hk.Before, "")
	after := scalarVal(hk.After, "")
	// Skip hunks that carry no actual change (e.g. a line-in-file task that
	// already matches, or an empty owner/mode "file attributes" diff).
	if before == after {
		return nil
	}
	var out []string
	if hk.BeforeHeader != "" {
		out = append(out, "--- "+hk.BeforeHeader)
	}
	if hk.AfterHeader != "" {
		out = append(out, "+++ "+hk.AfterHeader)
	}
	for _, l := range splitLines(before) {
		out = append(out, "- "+l)
	}
	for _, l := range splitLines(after) {
		out = append(out, "+ "+l)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func splitLines(s string) []string {
	if s == "" {
		return []string{""}
	}
	lines := strings.Split(s, "\n")
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func scalarVal(raw json.RawMessage, dflt string) string {
	if len(raw) == 0 {
		return dflt
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var bb bytes.Buffer
	if json.Compact(&bb, raw) == nil {
		return bb.String()
	}
	return dflt
}

// rule shortens a header to the trailing length of the mixed-rule separator.
const ruleLen = 56

func rule(name string) string {
	suffix := " " + strings.Repeat("*", ruleLen)
	if len(name) >= ruleLen {
		return name + "*"
	}
	return name + suffix[len(name):]
}

type hostOut struct {
	host   string
	result string
	failed bool
	msg    string
}

func sortedHosts(hosts map[string]*hostResult) []hostOut {
	out := make([]hostOut, 0, len(hosts))
	for h, hr := range hosts {
		result := "ok"
		switch {
		case hr.Unreachable:
			result = "unreachable"
		case hr.Failed:
			result = "failed"
		case hr.Changed:
			result = "changed"
		case hr.Skipped:
			result = "skipped"
		}
		out = append(out, hostOut{host: h, result: result, failed: hr.Failed, msg: hr.Msg})
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].host < out[j-1].host; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func sortedStatHosts(stats map[string]*statLine) []string {
	out := make([]string, 0, len(stats))
	for h := range stats {
		out = append(out, h)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
