// Package runtimeversion answers which octo release a runtime image is, and
// whether one release is behind another.
//
// Releases are compared by tag rather than by full image reference, because a
// mirrored registry gives the same runtime a different path.
package runtimeversion

import (
	"regexp"
	"strconv"
	"strings"
)

// Current is the release an install deploys: the configured version when there
// is one, and otherwise the tag its image reference carries. The version wins
// because an operator who sets it explicitly is believed over a tag that may be
// `latest`, and because a digest-pinned image has no tag to read at all.
func Current(version, image string) string {
	if version != "" {
		return version
	}
	return ImageTag(image)
}

// ImageTag is the tag part of a container image reference, or "" when the
// reference is untagged or pinned by digest. The colon searched for is the last
// one, and only past the final slash, so a registry with a port
// (registry:5000/octo/runtime) is not mistaken for a tag.
//
// Both spellings of a digest are turned away: `repo@sha256:…`, and the bare
// `sha256:…` the kubelet reports for a pod started from one, which has no `@` and
// would otherwise split into a name of "sha256" and a "tag" of hex.
func ImageTag(image string) string {
	if strings.Contains(image, "@") {
		return ""
	}
	name := image
	if i := strings.LastIndex(image, "/"); i >= 0 {
		name = image[i+1:]
	}
	i := strings.LastIndex(name, ":")
	if i < 0 {
		return ""
	}
	if algo := name[:i]; algo == "sha256" || algo == "sha512" {
		return ""
	}
	return name[i+1:]
}

// Behind reports whether deployed is an OLDER release than current.
//
// Older, not merely different: a deployment rolled onto a build the install has
// not caught up with is ahead, and calling it behind would tell someone to
// downgrade. Anything that is not a dotted release number — `dev`, a branch
// build, a prerelease — is incomparable, and an unknown on either side is never
// behind.
func Behind(deployed, current string) bool {
	if deployed == "" || current == "" || deployed == current {
		return false
	}
	a, b := release(deployed), release(current)
	if a == nil || b == nil {
		return false
	}
	for i := range max(len(a), len(b)) {
		left, right := part(a, i), part(b, i)
		if left != right {
			return left < right
		}
	}
	return false
}

// releasePattern is a dotted release number with an optional leading `v`.
// Anything after the numbers — `-rc1`, `+build` — fails the match, since ordering
// prereleases from a tag alone is a judgement this package does not make.
var releasePattern = regexp.MustCompile(`^v?(\d+(?:\.\d+)*)$`)

// release splits a release tag into its numeric parts, or nil when it is not one.
func release(tag string) []int {
	m := releasePattern.FindStringSubmatch(strings.TrimSpace(tag))
	if m == nil {
		return nil
	}
	fields := strings.Split(m[1], ".")
	ret := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil
		}
		ret = append(ret, n)
	}
	return ret
}

// part is the i-th release component, a missing one counting as 0 so that 0.8
// and 0.8.0 are the same release.
func part(parts []int, i int) int {
	if i < len(parts) {
		return parts[i]
	}
	return 0
}
