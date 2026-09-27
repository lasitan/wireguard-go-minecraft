// Package update checks GitHub Releases for newer wireguard-mc builds and
// replaces the running binary in place.
package update

import (
	"strconv"
	"strings"
	"sync/atomic"
)

var current atomic.Value

// SetCurrent records the version this binary was built as (from main.Version).
func SetCurrent(v string) { current.Store(Normalize(v)) }

// Current returns the normalized build version, or "" when unknown.
func Current() string {
	v, _ := current.Load().(string)
	return v
}

// Normalize strips whitespace and a leading "v" so "v2.0.3" == "2.0.3".
func Normalize(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// Valid returns the normalized version when it parses as semver, else "".
// Pre-2.0.3 agents send a protocol tag ("wg-mc-agent/2") instead.
func Valid(v string) string {
	if _, ok := parse(v); !ok {
		return ""
	}
	return Normalize(v)
}

type semver struct {
	nums [3]int
	pre  string
}

func parse(v string) (semver, bool) {
	v = Normalize(v)
	if v == "" {
		return semver{}, false
	}
	var s semver
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		s.pre = v[i+1:]
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) > 3 {
		return semver{}, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return semver{}, false
		}
		s.nums[i] = n
	}
	return s, true
}

// Newer reports whether candidate is strictly newer than base.
// Unparseable versions never compare as newer, so dev builds don't nag.
func Newer(candidate, base string) bool {
	c, ok1 := parse(candidate)
	b, ok2 := parse(base)
	if !ok1 || !ok2 {
		return false
	}
	for i := range c.nums {
		if c.nums[i] != b.nums[i] {
			return c.nums[i] > b.nums[i]
		}
	}
	// Same numbers: a release beats its prerelease (2.0.3 > 2.0.3-dev).
	return c.pre == "" && b.pre != ""
}
