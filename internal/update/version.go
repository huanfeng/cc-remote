package update

import (
	"strconv"
	"strings"
)

// semver is a parsed "vMAJOR.MINOR.PATCH[-PRERELEASE][+BUILD]" version.
type semver struct {
	major, minor, patch int
	pre                 string
}

func parseSemver(v string) (semver, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	var s semver
	if i := strings.IndexByte(v, '-'); i >= 0 {
		s.pre, v = v[i+1:], v[:i]
		if s.pre == "" {
			return semver{}, false
		}
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p == "" {
			return semver{}, false
		}
		nums[i] = n
	}
	s.major, s.minor, s.patch = nums[0], nums[1], nums[2]
	return s, true
}

// compareVersions returns -1, 0 or 1 comparing a with b. ok is false when
// either side is not a semantic version (for example the "dev" build).
func compareVersions(a, b string) (cmp int, ok bool) {
	x, okA := parseSemver(a)
	y, okB := parseSemver(b)
	if !okA || !okB {
		return 0, false
	}
	for _, d := range [][2]int{{x.major, y.major}, {x.minor, y.minor}, {x.patch, y.patch}} {
		if d[0] != d[1] {
			if d[0] < d[1] {
				return -1, true
			}
			return 1, true
		}
	}
	return comparePrerelease(x.pre, y.pre), true
}

// comparePrerelease follows SemVer 2.0 precedence: a release is newer than any
// prerelease; identifiers compare numerically when both are numbers.
func comparePrerelease(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return 1
	}
	if b == "" {
		return -1
	}
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] == bs[i] {
			continue
		}
		an, aErr := strconv.Atoi(as[i])
		bn, bErr := strconv.Atoi(bs[i])
		switch {
		case aErr == nil && bErr == nil:
			if an < bn {
				return -1
			}
			return 1
		case aErr == nil:
			return -1
		case bErr == nil:
			return 1
		case as[i] < bs[i]:
			return -1
		default:
			return 1
		}
	}
	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	}
	return 0
}

// normalizeTag turns "0.2.0" or "v0.2.0" into the release tag "v0.2.0".
func normalizeTag(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}
