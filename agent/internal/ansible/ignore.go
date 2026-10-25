package ansible

import (
	"regexp"
	"strings"
)

// FilterIgnored removes changed tasks and resources that match any of the
// newline/comma separated glob patterns, and downgrades a check run to "ok"
// when every remaining change is ignored. Patterns are matched case
// insensitively against the task name, the host and the resource target
// ("*" matches any sequence including "/").
func FilterIgnored(res *Result, patterns string) {
	matches := compilePatterns(patterns)
	if len(matches) == 0 {
		return
	}
	matchAny := func(values ...string) bool {
		for _, p := range matches {
			for _, v := range values {
				if p.MatchString(v) {
					return true
				}
			}
		}
		return false
	}

	keptTasks := res.ChangedTasks[:0]
	for _, t := range res.ChangedTasks {
		if matchAny(t.TaskName, t.Host) {
			continue
		}
		keptTasks = append(keptTasks, t)
	}
	removed := len(res.ChangedTasks) - len(keptTasks)
	res.ChangedTasks = keptTasks

	keptResources := res.Resources[:0]
	for _, r := range res.Resources {
		if matchAny(r.Target, r.Module+" "+r.Target) {
			continue
		}
		keptResources = append(keptResources, r)
	}
	res.Resources = keptResources

	if removed > 0 && res.Summary != nil {
		if n, ok := res.Summary["changed"]; ok {
			n -= removed
			if n < 0 {
				n = 0
			}
			res.Summary["changed"] = n
		}
	}
	if res.Status == StatusDrift && len(res.ChangedTasks) == 0 {
		res.Status = StatusOK
	}
}

func compilePatterns(raw string) []*regexp.Regexp {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == ';'
	})
	out := make([]*regexp.Regexp, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		quoted := regexp.QuoteMeta(f)
		quoted = strings.ReplaceAll(quoted, `\*`, `.*`)
		quoted = strings.ReplaceAll(quoted, `\?`, `.`)
		re, err := regexp.Compile("(?i)^" + quoted + "$")
		if err != nil {
			continue
		}
		out = append(out, re)
	}
	return out
}
