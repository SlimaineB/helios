package gitops

import (
	"strings"
	"testing"
)

func TestParseSV(t *testing.T) {
	cases := []struct {
		in      string
		want    sv
		invalid bool
	}{
		{"1.2.3", sv{major: 1, minor: 2, patch: 3}, false},
		{"v2.0.0", sv{major: 2}, false},
		{"V0.9.1", sv{minor: 9, patch: 1}, false},
		{"1.2", sv{major: 1, minor: 2}, false},
		{"3", sv{major: 3}, false},
		{"1.2.3-rc.1", sv{major: 1, minor: 2, patch: 3, pre: []string{"rc", "1"}}, false},
		{"1.2.3+build5", sv{major: 1, minor: 2, patch: 3}, false},
		{"latest", sv{}, true},
		{"release-2024", sv{}, true},
		{"v", sv{}, true},
		{"1.2.3.4", sv{}, true},
	}
	for _, c := range cases {
		got, ok := parseSV(c.in)
		if ok == c.invalid {
			t.Fatalf("parseSV(%q) ok=%v, want invalid=%v", c.in, ok, c.invalid)
		}
		if !ok {
			continue
		}
		if got.major != c.want.major || got.minor != c.want.minor || got.patch != c.want.patch {
			t.Errorf("parseSV(%q) = %d.%d.%d, want %d.%d.%d",
				c.in, got.major, got.minor, got.patch, c.want.major, c.want.minor, c.want.patch)
		}
		if len(got.pre) != len(c.want.pre) {
			t.Errorf("parseSV(%q) pre = %v, want %v", c.in, got.pre, c.want.pre)
		}
	}
}

func TestSVCmp(t *testing.T) {
	less := func(a, b string) {
		va, _ := parseSV(a)
		vb, _ := parseSV(b)
		if _, ok := parseSV(a); !ok {
			t.Fatalf("bad a %q", a)
		}
		if va.cmp(vb) >= 0 {
			t.Errorf("%s should sort below %s", a, b)
		}
	}
	less("1.0.0", "1.0.1")
	less("1.9.9", "1.10.0")
	less("1.2.0", "2.0.0")
	less("1.2.3-rc.1", "1.2.3")
	less("1.0.0-alpha", "1.0.0-alpha.1")
	less("1.0.0-alpha", "1.0.0-beta")
}

func TestParseConstraints(t *testing.T) {
	match := func(expr, tag string) {
		cons, err := parseConstraints(expr)
		if err != nil {
			t.Fatalf("parseConstraints(%q): %v", expr, err)
		}
		v, ok := parseSV(tag)
		if !ok {
			t.Fatalf("bad tag %q", tag)
		}
		if !satisfiesAll(cons, v) {
			t.Errorf("%q should match %q", expr, tag)
		}
	}
	nomatch := func(expr, tag string) {
		cons, err := parseConstraints(expr)
		if err != nil {
			t.Fatalf("parseConstraints(%q): %v", expr, err)
		}
		v, _ := parseSV(tag)
		if satisfiesAll(cons, v) {
			t.Errorf("%q should NOT match %q", expr, tag)
		}
	}

	match("*", "1.2.3")
	match("1.x", "1.5.0")
	match("1.x", "1.0.0")
	nomatch("1.x", "2.0.0")
	match("1.2.x", "1.2.9")
	nomatch("1.2.x", "1.3.0")
	match(">=1.2.0 <2.0.0", "1.9.1")
	nomatch(">=1.2.0 <2.0.0", "2.0.0")
	nomatch(">=1.2.0 <2.0.0", "1.1.0")
	match("^1.0.0", "1.5.0")
	nomatch("^1.0.0", "2.0.0")
	nomatch("^1.0.0", "0.9.0")
	match("~1.2", "1.2.9")
	nomatch("~1.2", "1.3.0")
	match("=v1.2.3", "1.2.3")
	match(">1.0.0 <=1.2.0", "1.2.0")
	nomatch(">1.0.0 <=1.2.0", "1.2.1")

	if _, err := parseConstraints(">=1.0.0 || <0.5.0"); err == nil {
		t.Error("OR ranges should be rejected")
	}
	if _, err := parseConstraints("nope"); err == nil {
		t.Error("garbage range should be rejected")
	}
}

func TestPickBestTag(t *testing.T) {
	names := []string{
		"v0.9.0", "v1.0.0", "v1.1.0", "v1.2.0",
		"latest",      // non-semver, must be ignored
		"release",     // non-semver, must be ignored
		"v1.2.0-rc.1", // prerelease below its release
	}
	best := func(expr string) string {
		tag, err := pickBestTag(expr, names)
		if err != nil {
			t.Fatalf("pickBestTag(%q): %v", expr, err)
		}
		return tag
	}
	if got := best(">=1.0.0"); got != "v1.2.0" {
		t.Errorf(">=1.0.0 picked %s, want v1.2.0", got)
	}
	if got := best("1.x"); got != "v1.2.0" {
		t.Errorf("1.x picked %s, want v1.2.0", got)
	}
	if got := best(">=1.0.0 <1.2.0"); got != "v1.1.0" {
		t.Errorf(">=1.0.0 <1.2.0 picked %s, want v1.1.0", got)
	}
	if got := best(">=1.2.0-rc.1"); got != "v1.2.0" {
		t.Errorf(">=1.2.0-rc.1 picked %s, want v1.2.0", got)
	}
	if _, err := pickBestTag(">=2.0.0", names); err == nil {
		t.Error("no matching tag should error")
	}
	if got := best("*"); got != "v1.2.0" {
		t.Errorf("* picked %s, want v1.2.0", got)
	}
}

func TestIsSemverRange(t *testing.T) {
	ranges := []string{">=1.2.0", "<2.0.0", "^1.0.0", "~1.2", "1.x", "1.2.x", "*", "=1.2.3"}
	refs := []string{"main", "feature-x", "v1.2.3", "1.2.3", "release/2.0", "feature/x", "master"}
	for _, r := range ranges {
		if !isSemverRange(r) {
			t.Errorf("isSemverRange(%q) = false, want true", r)
		}
	}
	for _, r := range refs {
		if isSemverRange(r) {
			t.Errorf("isSemverRange(%q) = true, want false", r)
		}
	}
}

func TestSortTagsByVersionDesc(t *testing.T) {
	in := []string{"v1.0.0", "latest", "v1.2.0", "v0.9.0", "v1.1.0"}
	sortTagsByVersionDesc(in)
	if in[0] != "v1.2.0" || in[1] != "v1.1.0" || in[2] != "v1.0.0" {
		t.Errorf("unexpected order: %v", in)
	}
	if !strings.EqualFold(in[3], "latest") && strings.HasPrefix(in[3], "v0.9.0") && strings.EqualFold(in[3], "latest") {
		t.Logf("ok %v", in)
	}
}
