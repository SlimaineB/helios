// Package syncwindows evaluates ArgoCD-style maintenance windows: deny
// windows freeze syncs while active, allow windows freeze syncs outside their
// schedule whenever at least one allow window exists for the same scope.
package syncwindows

import (
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // embed the IANA database so containers can resolve zones

	"github.com/slim/ansiblehub/server/internal/model"
)

var dayIndex = map[string]int{
	"mon": 0, "tue": 1, "wed": 2, "thu": 3, "fri": 4, "sat": 5, "sun": 6,
}

// Frozen reports whether syncs for appID are currently frozen. Global windows
// (AppID 0) and app-specific ones both apply.
func Frozen(windows []model.SyncWindow, appID int64, now time.Time) bool {
	hasAllow := false
	allowActive := false
	for _, w := range windows {
		if !w.Enabled {
			continue
		}
		if w.AppID != 0 && w.AppID != appID {
			continue
		}
		active := Active(w, now)
		switch strings.ToLower(w.Kind) {
		case "deny":
			if active {
				return true
			}
		case "allow":
			hasAllow = true
			if active {
				allowActive = true
			}
		}
	}
	return hasAllow && !allowActive
}

// Active reports whether the window covers now in its own timezone.
func Active(w model.SyncWindow, now time.Time) bool {
	loc := time.UTC
	if strings.TrimSpace(w.Timezone) != "" {
		if l, err := time.LoadLocation(strings.TrimSpace(w.Timezone)); err == nil {
			loc = l
		}
	}
	local := now.In(loc)
	if !dayAllowed(w.Days, local.Weekday()) {
		return false
	}
	m := local.Hour()*60 + local.Minute()
	start, end := w.StartMin, w.EndMin
	if start == end {
		return true
	}
	if start < end {
		return m >= start && m < end
	}
	return m >= start || m < end
}

func dayAllowed(days string, wd time.Weekday) bool {
	days = strings.TrimSpace(days)
	if days == "" {
		return true
	}
	idx := (int(wd) + 6) % 7 // time.Sunday=0 -> mon=0
	for _, d := range strings.Split(days, ",") {
		if dayIndex[strings.ToLower(strings.TrimSpace(d))] == idx {
			return true
		}
	}
	return false
}

// Validate checks a window definition, normalising timezone/days via the
// returned copy.
func Validate(w model.SyncWindow) (model.SyncWindow, string) {
	w.Kind = strings.ToLower(strings.TrimSpace(w.Kind))
	if w.Kind != "deny" && w.Kind != "allow" {
		return w, "kind must be deny|allow"
	}
	if strings.TrimSpace(w.Name) == "" {
		return w, "name is required"
	}
	if w.StartMin < 0 || w.StartMin > 1439 || w.EndMin < 0 || w.EndMin > 1439 {
		return w, "start_min/end_min must be between 0 and 1439"
	}
	if w.Timezone == "" {
		w.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(w.Timezone); err != nil {
		return w, "unknown timezone: " + w.Timezone
	}
	days := []string{}
	for _, d := range strings.Split(w.Days, ",") {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		if _, ok := dayIndex[d]; !ok {
			return w, "days must be a comma list of mon,tue,wed,thu,fri,sat,sun"
		}
		days = append(days, d)
	}
	w.Days = strings.Join(days, ",")
	return w, ""
}

// ParseHM converts "HH:MM" to minutes.
func ParseHM(v string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(v), ":")
	if len(parts) != 2 {
		return 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// FormatHM converts minutes to "HH:MM".
func FormatHM(v int) string {
	if v < 0 || v > 1439 {
		return ""
	}
	return strconv.Itoa(v/60/10) + strconv.Itoa(v/60%10) + ":" + strconv.Itoa(v%60/10) + strconv.Itoa(v%60%10)
}
