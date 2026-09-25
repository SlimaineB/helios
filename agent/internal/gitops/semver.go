package gitops

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// SemVer-compatible tag selection (ArgoCD style targetRevision):
//
//   - a plain target is a branch, tag or SHA used verbatim;
//   - a semver range like ">=1.2.0 <2.0.0", "1.x", "^1.0.0", "~1.2" or "*"
//     is resolved to the highest matching tag at sync time.
//
// Only the "^" / "~" cleverness of Node semantics is kept, plus multi-token
// AND ranges; "||" (OR) is intentionally unsupported.

type sv struct {
	major, minor, patch int
	pre                 []string // dot-separated pre-release identifiers
}

func parseSV(s string) (sv, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	if i := strings.IndexByte(s, '+'); i >= 0 { // drop build metadata
		s = s[:i]
	}
	var out sv
	if i := strings.IndexByte(s, '-'); i >= 0 {
		out.pre = strings.Split(s[i+1:], ".")
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return sv{}, false
	}
	nums := make([]int, 0, 3)
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 {
			return sv{}, false
		}
		nums = append(nums, n)
	}
	for i := 0; i < 3; i++ {
		switch i {
		case 0:
			out.major = num(nums, i)
		case 1:
			out.minor = num(nums, i)
		case 2:
			out.patch = num(nums, i)
		}
	}
	if len(nums) == 0 {
		return sv{}, false
	}
	return out, true
}

func num(nums []int, i int) int {
	if i < len(nums) {
		return nums[i]
	}
	return 0
}

// cmp returns -1, 0 or 1 (pre-release versions sort below their release).
func (a sv) cmp(b sv) int {
	if c := cmpInt(a.major, b.major); c != 0 {
		return c
	}
	if c := cmpInt(a.minor, b.minor); c != 0 {
		return c
	}
	if c := cmpInt(a.patch, b.patch); c != 0 {
		return c
	}
	if len(a.pre) == 0 && len(b.pre) == 0 {
		return 0
	}
	if len(a.pre) == 0 {
		return 1
	}
	if len(b.pre) == 0 {
		return -1
	}
	n := min(len(a.pre), len(b.pre))
	for i := 0; i < n; i++ {
		if c := cmpIdent(a.pre[i], b.pre[i]); c != 0 {
			return c
		}
	}
	return cmpInt(len(a.pre), len(b.pre))
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// cmpIdent compares pre-release identifiers: numeric ones numerically, others
// lexicographically (numeric < alphanumeric).
func cmpIdent(a, b string) int {
	an, aerr := strconv.Atoi(a)
	bn, berr := strconv.Atoi(b)
	switch {
	case aerr == nil && berr == nil:
		return cmpInt(an, bn)
	case aerr == nil:
		return -1
	case berr == nil:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

// constraint is a single comparison against a version.
type constraint struct {
	op                  string // >, >=, <, <=, =
	major, minor, patch int
}

func (c constraint) satisfies(v sv) bool {
	ref := sv{major: c.major, minor: c.minor, patch: c.patch}
	switch c.op {
	case ">":
		return v.cmp(ref) > 0
	case ">=":
		return v.cmp(ref) >= 0
	case "<":
		return v.cmp(ref) < 0
	case "<=":
		return v.cmp(ref) <= 0
	default: // =
		return v.cmp(ref) == 0
	}
}

// parseConstraints expands an expression into a list of AND-ed comparisons.
// Tokens may carry operators (>= <= > < = ^ ~) or be bare "1.x" wildcards.
func parseConstraints(expr string) ([]constraint, error) {
	var out []constraint
	tokens := splitTokens(expr)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty semver range %q", expr)
	}
	for _, tok := range tokens {
		rest := strings.TrimSpace(tok)
		if rest == "||" || strings.Contains(rest, "||") {
			return nil, fmt.Errorf("OR (||) ranges are not supported: %q", expr)
		}
		op := ""
		for _, p := range []string{">=", "<=", "^", "~", ">", "<", "="} {
			if strings.HasPrefix(rest, p) {
				op, rest = p, rest[len(p):]
				break
			}
		}
		rest = strings.TrimSpace(rest)
		if rest == "" {
			return nil, fmt.Errorf("missing version after operator %q", op)
		}
		// strip pre-release/build suffixes from the reference version: a
		// constraint compares against the core, e.g. ">=1.2.0-rc.1" == ">=1.2.0".
		if i := strings.IndexAny(rest, "-+"); i >= 0 {
			rest = rest[:i]
		}
		rest = strings.TrimPrefix(rest, "v")
		rest = strings.TrimPrefix(rest, "V")
		if rest == "*" || strings.EqualFold(rest, "x") {
			if op != "" {
				return nil, fmt.Errorf("operator %q cannot be combined with wildcard %q", op, tok)
			}
			continue // matches anything
		}
		parts := strings.Split(strings.ToLower(rest), ".")
		nums := make([]int, 0, 3)
		ended := false
		for _, p := range parts {
			if p == "x" || p == "*" || p == "~" {
				ended = true
				break
			}
			n, err := strconv.Atoi(p)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("invalid semver token %q", tok)
			}
			nums = append(nums, n)
		}
		if ended {
			if len(nums) > 2 || len(nums) == len(parts) {
				return nil, fmt.Errorf("wildcard must be the trailing part: %q", tok)
			}
			// "1.x" -> >=1.0.0 <2.0.0 ; "1.2.x" -> >=1.2.0 <1.3.0
			lo := sv{}
			if len(nums) >= 1 {
				lo.major = nums[0]
			}
			if len(nums) >= 2 {
				lo.minor = nums[1]
			}
			hi := lo
			if len(nums) == 1 {
				hi.major++
				hi.minor = 0
			} else {
				hi.minor++
			}
			out = append(out, constraint{op: ">=", major: lo.major, minor: lo.minor, patch: lo.patch})
			out = append(out, constraint{op: "<", major: hi.major, minor: hi.minor, patch: hi.patch})
			continue
		}
		v := sv{}
		for i, n := range nums {
			switch i {
			case 0:
				v.major = n
			case 1:
				v.minor = n
			case 2:
				v.patch = n
			}
		}
		switch op {
		case "^":
			out = append(out, constraint{op: ">=", major: v.major, minor: v.minor, patch: v.patch})
			out = append(out, constraint{op: "<", major: v.major + 1, minor: 0, patch: 0})
		case "~":
			out = append(out, constraint{op: ">=", major: v.major, minor: v.minor, patch: v.patch})
			out = append(out, constraint{op: "<", major: v.major, minor: v.minor + 1, patch: 0})
		default:
			if op == "" {
				op = "=" // bare version: exact
			}
			out = append(out, constraint{op: op, major: v.major, minor: v.minor, patch: v.patch})
		}
	}
	return out, nil
}

func splitTokens(expr string) []string {
	fields := strings.FieldsFunc(expr, func(r rune) bool { return r == ' ' || r == '\t' || r == ',' })
	var out []string
	for _, f := range fields {
		if strings.TrimSpace(f) != "" {
			out = append(out, f)
		}
	}
	return out
}

// isSemverRange reports whether a target should be resolved as a semver
// range instead of used verbatim as a ref name.
func isSemverRange(t string) bool {
	s := strings.TrimSpace(t)
	if s == "" {
		return false
	}
	for _, p := range []string{">=", "<=", "^", "~", ">", "<", "="} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	if strings.Contains(s, "*") {
		return true
	}
	// "1.x" / "1.2.x" (leading digit + an x in a bare token) counts as a range;
	// "feature-x" is not.
	first := s
	if i := strings.IndexAny(s, " \t,"); i >= 0 {
		first = s[:i]
	}
	return len(first) > 1 && first[0] >= '0' && first[0] <= '9' && strings.ContainsAny(first, "xX")
}

// pickBestTag returns the name of the highest semver tag matching expr.
// Pre-release tags are only eligible when the expression itself mentions a
// pre-release, so a plain ">=1.0.0" never selects an RC.
func pickBestTag(expr string, names []string) (string, error) {
	cons, err := parseConstraints(expr)
	if err != nil {
		return "", err
	}
	allowPre := strings.Contains(expr, "-")
	best, bestV := "", sv{}
	for _, n := range names {
		v, ok := parseSV(n)
		if !ok || (!allowPre && len(v.pre) > 0) || !satisfiesAll(cons, v) {
			continue
		}
		if best == "" || v.cmp(bestV) > 0 {
			best, bestV = n, v
		}
	}
	if best == "" {
		return "", fmt.Errorf("no semver tag matches %q", expr)
	}
	return best, nil
}

func satisfiesAll(cons []constraint, v sv) bool {
	if len(cons) == 0 {
		return true // bare "*" matches everything
	}
	for _, c := range cons {
		if !c.satisfies(v) {
			return false
		}
	}
	return true
}

// latestTag lists remote tags and resolves the highest one matching expr.
func (r Repo) latestTag(ctx context.Context, c Creds, env []string, expr string) (string, error) {
	args := gitArgs(c.InsecureSkipVerify,
		"git", "ls-remote", "--tags", "--refs", remoteURL(r.URL, c))
	out, err := runGit(ctx, env, args...)
	if err != nil {
		return "", fmt.Errorf("git ls-remote --tags %s: %w: %s", r.URL, err, strings.TrimSpace(string(out)))
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		i := strings.Index(line, "refs/tags/")
		if i < 0 {
			continue
		}
		name := line[i+len("refs/tags/"):]
		if name == "" || strings.HasSuffix(name, "^{}") {
			continue
		}
		names = append(names, name)
	}
	tag, err := pickBestTag(expr, names)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w (available tags: %s)", expr, err, strings.Join(names, ", "))
	}
	return tag, nil
}

// SortTags desc is exposed for tests.
func sortTagsByVersionDesc(names []string) {
	sort.SliceStable(names, func(i, j int) bool {
		vi, oki := parseSV(names[i])
		vj, okj := parseSV(names[j])
		if !oki && !okj {
			return names[i] < names[j]
		}
		if !oki {
			return false
		}
		if !okj {
			return true
		}
		if vi.cmp(vj) != 0 {
			return vi.cmp(vj) > 0
		}
		return names[i] > names[j]
	})
}
