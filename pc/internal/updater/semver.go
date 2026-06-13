package updater

import (
	"strconv"
	"strings"
)

// Compare orders two version strings of the form "v1.2.3" (the leading "v" and
// any "-suffix"/"+build" are ignored). It returns -1 if a < b, 0 if equal, and
// +1 if a > b. Non-numeric or empty versions (notably the "dev" sentinel of an
// un-versioned local build) sort below every real release, so a packaged build
// always reads as an available update over a dev binary.
func Compare(a, b string) int {
	am, an := parse(a)
	bm, bn := parse(b)
	if !an || !bn {
		switch {
		case an && !bn:
			return 1 // a is a real version, b is not
		case !an && bn:
			return -1
		default:
			return 0 // neither parses — treat as equal
		}
	}
	for i := 0; i < 3; i++ {
		if am[i] != bm[i] {
			if am[i] < bm[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// parse extracts [major, minor, patch] from a version string. ok is false when
// the string isn't a recognizable semantic version.
func parse(v string) (nums [3]int, ok bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return nums, false
	}
	// Drop pre-release / build metadata; we compare on the numeric core only.
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	for i := 0; i < 3 && i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return [3]int{}, false
		}
		nums[i] = n
	}
	return nums, true
}
