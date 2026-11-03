package runtimeversion

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
)

// supportedJSON lists the runtime releases a deployment may be pinned to,
// newest first. It starts at 0.11.13, the first release built with Go 1.27.3
// (CVE-2026-111943): nothing older is safe to pin a workload to.
//
//go:embed supported.json
var supportedJSON []byte

// supported is supportedJSON parsed once.
var supported = mustParseSupported(supportedJSON)

func mustParseSupported(raw []byte) []string {
	var doc struct {
		Supported []string `json:"supported"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		panic(fmt.Sprintf("runtimeversion: supported.json: %v", err))
	}
	for _, v := range doc.Supported {
		if !IsRelease(v) {
			panic(fmt.Sprintf("runtimeversion: supported.json: %q is not a release", v))
		}
	}
	return doc.Supported
}

// Pinnable is what a deployment on an install deploying current may be pinned
// to, newest first: current itself, and every supported release no newer than
// it. A release newer than the install's may not be in its registry.
func Pinnable(current string) []string {
	var ret []string
	if IsRelease(current) {
		ret = append(ret, current)
	}
	for _, v := range supported {
		if v != current && (current == "" || !IsRelease(current) || !Behind(current, v)) {
			ret = append(ret, v)
		}
	}
	slices.SortStableFunc(ret, func(a, b string) int {
		switch {
		case Behind(a, b):
			return 1
		case Behind(b, a):
			return -1
		}
		return 0
	})
	return ret
}

// CanPin reports whether a deployment on an install deploying current may be
// pinned to version.
func CanPin(version, current string) bool {
	return slices.Contains(Pinnable(current), version)
}
