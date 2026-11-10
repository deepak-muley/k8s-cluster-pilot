// Package semver parses Kubernetes-style versions without treating a missing
// patch as a fabricated patch release.
package semver

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a parsed major.minor[.patch] version.
// HasPatch is false when the input omitted the patch, so callers must not
// present Patch 0 as a version the user asked for.
type Version struct {
	Major    int
	Minor    int
	Patch    int
	HasPatch bool
	Original string
	Pre      string
}

// Parse accepts "1.33", "v1.32.2", and "v1.32.2+k3s1".
func Parse(raw string) (Version, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Version{}, fmt.Errorf("empty version")
	}
	original := s
	s = strings.TrimPrefix(s, "v")
	pre := ""
	if i := strings.IndexAny(s, "+-"); i >= 0 {
		pre = s[i:]
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return Version{}, fmt.Errorf("unsupported version %q", raw)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil || major < 0 {
		return Version{}, fmt.Errorf("unsupported version %q", raw)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil || minor < 0 {
		return Version{}, fmt.Errorf("unsupported version %q", raw)
	}
	v := Version{Major: major, Minor: minor, Original: original, Pre: pre}
	if len(parts) == 3 {
		patch, err := strconv.Atoi(parts[2])
		if err != nil || patch < 0 {
			return Version{}, fmt.Errorf("unsupported version %q", raw)
		}
		v.Patch = patch
		v.HasPatch = true
	}
	return v, nil
}

// MustParse parses s or panics. It is for tests.
func MustParse(s string) Version {
	v, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return v
}

// MinorString returns "major.minor" with no patch.
func (v Version) MinorString() string {
	return fmt.Sprintf("%d.%d", v.Major, v.Minor)
}

// CompareMinor compares major then minor. Patch and pre-release are ignored.
// It returns -1, 0, or 1.
func (v Version) CompareMinor(o Version) int {
	if v.Major != o.Major {
		if v.Major < o.Major {
			return -1
		}
		return 1
	}
	if v.Minor != o.Minor {
		if v.Minor < o.Minor {
			return -1
		}
		return 1
	}
	return 0
}

// SameMajor reports whether both versions share a major version.
func (v Version) SameMajor(o Version) bool {
	return v.Major == o.Major
}

// MinorLag returns how many minors v is behind newer. The boolean is false when
// the majors differ, because the skew policy's minor limits are same-major.
func (v Version) MinorLag(newer Version) (int, bool) {
	if v.Major != newer.Major {
		return 0, false
	}
	return newer.Minor - v.Minor, true
}

// WithMinor returns a patchless version at the same major and the given minor.
func (v Version) WithMinor(minor int) Version {
	return Version{Major: v.Major, Minor: minor, Original: fmt.Sprintf("%d.%d", v.Major, minor)}
}
