package utils

import (
	"fmt"
	"strconv"
	"strings"
)

type semver struct {
	major int
	minor int
	patch int
	pre   string
}

func GT(a, b string) bool  { return compareSemver(a, b) > 0 }
func GTE(a, b string) bool { return compareSemver(a, b) >= 0 }
func LT(a, b string) bool  { return compareSemver(a, b) < 0 }
func LTE(a, b string) bool { return compareSemver(a, b) <= 0 }
func Order(a, b string) int {
	cmp := compareSemver(a, b)
	switch {
	case cmp < 0:
		return -1
	case cmp > 0:
		return 1
	default:
		return 0
	}
}

func Satisfies(version, rng string) bool {
	rng = strings.TrimSpace(rng)
	if rng == "" {
		return false
	}
	parts := strings.Fields(strings.ReplaceAll(rng, ",", " "))
	for _, part := range parts {
		if !satisfiesPart(version, part) {
			return false
		}
	}
	return true
}

func satisfiesPart(version, part string) bool {
	switch {
	case strings.HasPrefix(part, ">="):
		return GTE(version, strings.TrimSpace(part[2:]))
	case strings.HasPrefix(part, "<="):
		return LTE(version, strings.TrimSpace(part[2:]))
	case strings.HasPrefix(part, ">"):
		return GT(version, strings.TrimSpace(part[1:]))
	case strings.HasPrefix(part, "<"):
		return LT(version, strings.TrimSpace(part[1:]))
	case strings.HasPrefix(part, "^"):
		base, err := parseSemver(strings.TrimSpace(part[1:]))
		if err != nil {
			return false
		}
		upper := fmt.Sprintf("%d.0.0", base.major+1)
		return GTE(version, strings.TrimSpace(part[1:])) && LT(version, upper)
	case strings.HasPrefix(part, "~"):
		base, err := parseSemver(strings.TrimSpace(part[1:]))
		if err != nil {
			return false
		}
		upper := fmt.Sprintf("%d.%d.0", base.major, base.minor+1)
		return GTE(version, strings.TrimSpace(part[1:])) && LT(version, upper)
	default:
		return compareSemver(version, part) == 0
	}
}

func compareSemver(a, b string) int {
	av, aerr := parseSemver(a)
	bv, berr := parseSemver(b)
	if aerr != nil || berr != nil {
		return strings.Compare(strings.TrimSpace(a), strings.TrimSpace(b))
	}
	if av.major != bv.major {
		return compareInt(av.major, bv.major)
	}
	if av.minor != bv.minor {
		return compareInt(av.minor, bv.minor)
	}
	if av.patch != bv.patch {
		return compareInt(av.patch, bv.patch)
	}
	if av.pre == bv.pre {
		return 0
	}
	if av.pre == "" {
		return 1
	}
	if bv.pre == "" {
		return -1
	}
	return strings.Compare(av.pre, bv.pre)
}

func parseSemver(value string) (semver, error) {
	value = strings.TrimSpace(strings.TrimPrefix(value, "v"))
	core, pre, _ := strings.Cut(value, "-")
	core, _, _ = strings.Cut(core, "+")
	parts := strings.Split(core, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return semver{}, fmt.Errorf("invalid semver")
	}
	get := func(index int) (int, error) {
		if index >= len(parts) {
			return 0, nil
		}
		return strconv.Atoi(parts[index])
	}
	major, err := get(0)
	if err != nil {
		return semver{}, err
	}
	minor, err := get(1)
	if err != nil {
		return semver{}, err
	}
	patch, err := get(2)
	if err != nil {
		return semver{}, err
	}
	return semver{major: major, minor: minor, patch: patch, pre: pre}, nil
}

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
