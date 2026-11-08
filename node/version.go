// Package node picks the Node.js version for a project and runs Node through
// the fnm helper. The version a project wants lives in its own files
// (.nvmrc, .node-version, package.json engines.node), not in the devctl
// database.
package node

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version is one installed Node.js release.
type Version struct {
	Major, Minor, Patch int
}

func (v Version) String() string {
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}

func (v Version) less(o Version) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}

// ParseVersion parses an exact version such as "v22.11.0" or "22.11.0".
func ParseVersion(s string) (Version, bool) {
	p, n, ok := parsePartial(s)
	if !ok || n != 3 {
		return Version{}, false
	}
	return p, true
}

// ltsCodenames maps each LTS line codename to its major version.
var ltsCodenames = map[string]int{
	"argon": 4, "boron": 6, "carbon": 8, "dubnium": 10, "erbium": 12, "fermium": 14,
	"gallium": 16, "hydrogen": 18, "iron": 20, "jod": 22, "krypton": 24,
}

// Even majors become LTS lines; odd majors never do.
func isLTS(v Version) bool { return v.Major >= 4 && v.Major%2 == 0 }

func anyVersion(Version) bool { return true }

// Resolve returns the newest installed version that satisfies want. want is a
// version file value: "20", "v20.11.1", "lts/*", "lts/iron", "latest", or a
// semver range such as ">=20", "^18.17", or "18 || 20". An empty want picks
// the default: the newest LTS line, else the newest version.
func Resolve(want string, installed []Version) (Version, bool) {
	if strings.TrimSpace(want) == "" {
		if v, ok := newest(installed, isLTS); ok {
			return v, true
		}
		return newest(installed, anyVersion)
	}
	match, ok := matcher(want)
	if !ok {
		return Version{}, false
	}
	return newest(installed, match)
}

// Valid reports whether want is a version value devctl can resolve.
func Valid(want string) bool {
	_, ok := matcher(want)
	return ok
}

func newest(installed []Version, match func(Version) bool) (Version, bool) {
	var best Version
	found := false
	for _, v := range installed {
		if match(v) && (!found || best.less(v)) {
			best, found = v, true
		}
	}
	return best, found
}

func matcher(want string) (func(Version) bool, bool) {
	s := strings.ToLower(strings.TrimSpace(want))
	switch s {
	case "":
		return nil, false
	case "node", "latest", "current", "stable":
		return anyVersion, true
	case "lts", "lts/*", "lts-*":
		return isLTS, true
	}
	if name, ok := cutLTS(s); ok {
		major, known := ltsCodenames[name]
		if !known {
			return nil, false
		}
		return func(v Version) bool { return v.Major == major }, true
	}
	return parseRange(s)
}

func cutLTS(s string) (string, bool) {
	if name, ok := strings.CutPrefix(s, "lts/"); ok {
		return name, true
	}
	return strings.CutPrefix(s, "lts-")
}

// interval is the half-open version range [lo, hi). bounded=false means no
// upper limit. Every npm comparator is one interval, and a space-separated
// set of comparators is their intersection, so it is one interval too.
type interval struct {
	lo, hi  Version
	bounded bool
}

func (r interval) contains(v Version) bool {
	return !v.less(r.lo) && (!r.bounded || v.less(r.hi))
}

func (r interval) intersect(o interval) interval {
	out := r
	if out.lo.less(o.lo) {
		out.lo = o.lo
	}
	if o.bounded && (!out.bounded || o.hi.less(out.hi)) {
		out.hi, out.bounded = o.hi, true
	}
	return out
}

var (
	everything = interval{}
	nothing    = interval{bounded: true}
	opSpace    = regexp.MustCompile(`(>=|<=|>|<|=|\^|~)\s+`)
)

func parseRange(s string) (func(Version) bool, bool) {
	var alts []interval
	for _, alt := range strings.Split(s, "||") {
		r, ok := parseConjunction(strings.TrimSpace(alt))
		if !ok {
			return nil, false
		}
		alts = append(alts, r)
	}
	return func(v Version) bool {
		for _, r := range alts {
			if r.contains(v) {
				return true
			}
		}
		return false
	}, true
}

func parseConjunction(s string) (interval, bool) {
	fields := strings.Fields(opSpace.ReplaceAllString(s, "$1"))
	if len(fields) == 0 {
		return interval{}, false
	}
	if len(fields) == 3 && fields[1] == "-" {
		return parseHyphen(fields[0], fields[2])
	}
	out := everything
	for _, f := range fields {
		r, ok := parseComparator(f)
		if !ok {
			return interval{}, false
		}
		out = out.intersect(r)
	}
	return out, true
}

func parseHyphen(from, to string) (interval, bool) {
	lo, _, ok := parsePartial(from)
	if !ok {
		return interval{}, false
	}
	hi, n, ok := parsePartial(to)
	if !ok {
		return interval{}, false
	}
	if n == 0 {
		return interval{lo: lo}, true
	}
	return interval{lo: lo, hi: bump(hi, n), bounded: true}, true
}

func parseComparator(tok string) (interval, bool) {
	op := ""
	for _, p := range []string{">=", "<=", ">", "<", "=", "^", "~"} {
		if rest, ok := strings.CutPrefix(tok, p); ok {
			op, tok = p, rest
			break
		}
	}
	p, n, ok := parsePartial(tok)
	if !ok {
		return interval{}, false
	}
	if n == 0 {
		if op == ">" || op == "<" {
			return nothing, true
		}
		return everything, true
	}
	switch op {
	case "", "=":
		return interval{lo: p, hi: bump(p, n), bounded: true}, true
	case ">=":
		return interval{lo: p}, true
	case ">":
		return interval{lo: bump(p, n)}, true
	case "<":
		return interval{hi: p, bounded: true}, true
	case "<=":
		return interval{hi: bump(p, n), bounded: true}, true
	case "^":
		switch {
		case p.Major > 0 || n == 1:
			return interval{lo: p, hi: bump(p, 1), bounded: true}, true
		case p.Minor > 0 || n == 2:
			return interval{lo: p, hi: bump(p, 2), bounded: true}, true
		default:
			return interval{lo: p, hi: bump(p, 3), bounded: true}, true
		}
	default: // "~"
		if n == 1 {
			return interval{lo: p, hi: bump(p, 1), bounded: true}, true
		}
		return interval{lo: p, hi: bump(p, 2), bounded: true}, true
	}
}

// parsePartial parses "20", "20.11", "v20.11.1", "20.x", or "*". n is the
// number of numeric parts given; missing parts are zero.
func parsePartial(s string) (Version, int, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if s == "" {
		return Version{}, 0, false
	}
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return Version{}, 0, false
	}
	var nums [3]int
	n := 0
	for _, part := range parts {
		if part == "x" || part == "X" || part == "*" {
			break
		}
		num, err := strconv.Atoi(part)
		if err != nil || num < 0 {
			return Version{}, 0, false
		}
		nums[n] = num
		n++
	}
	return Version{nums[0], nums[1], nums[2]}, n, true
}

// bump returns the smallest version above every version that starts with the
// first n parts of p.
func bump(p Version, n int) Version {
	switch n {
	case 1:
		return Version{p.Major + 1, 0, 0}
	case 2:
		return Version{p.Major, p.Minor + 1, 0}
	default:
		return Version{p.Major, p.Minor, p.Patch + 1}
	}
}
