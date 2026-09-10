// Package projectfile holds the two rules the orchestrator needs to treat an
// integration as a folder of files rather than a single definition column: what
// a file is, decided from its path, and how several config files fold into one
// definition.
//
// Both rules already exist in the runtime (runtime/core/runtime/config.go). They
// are restated here because the orchestrator is a separate Go module that
// deliberately requires nothing from the runtime — the same reason
// deployment/envports.go hand-rolls its minimal YAML parse. The duplication is
// the price of that boundary; testdata/merge_cases is what keeps the two honest.
package projectfile

import (
	"path"
	"strings"
)

// testSuffix marks a YAML file as a dolphin suite rather than config:
// orders_test.yaml tests orders.yaml. Mirrors runtime.TestFileSuffix.
const testSuffix = "_test"

// Role is what a file is within a project. It is derived from the path and
// stored on the row, so selecting an integration's flow files is an index hit
// instead of a scan-and-classify.
type Role string

const (
	// RoleConfig is a flow file: the runtime merges these into one config.
	RoleConfig Role = "config"
	// RoleTest is a dolphin suite, which the runtime walks past.
	RoleTest Role = "test"
	// RoleResource is everything else — the env files and templates a running
	// integration loads by name.
	RoleResource Role = "resource"
)

// Classify decides what a file is from where it sits.
//
// The rule is the runtime's: a directory config is every .yaml/.yml file
// *directly* in the folder, minus the test files. Non-recursive is the part
// that carries weight here — it is what makes templates/welcome.tmpl and
// skills/refunds.md resources rather than config, without anyone declaring them
// as such.
func Classify(p string) Role {
	if strings.Contains(p, "/") {
		return RoleResource
	}
	if !isYAML(p) {
		return RoleResource
	}
	if IsTestFile(p) {
		return RoleTest
	}
	return RoleConfig
}

// IsTestFile reports whether name is a suite rather than config, the way
// orders_test.go tests orders.go. dolphin discovers exactly the files the config
// loader skips, so this and the runtime's copy must never disagree.
//
// The suffix match is case-sensitive while the extension match is not. That is
// not a choice made here — it is what runtime.IsTestFile does, and mirroring it
// exactly is the whole job. orders_TEST.yaml is config to the runtime, so it is
// config here too, however odd that reads.
func IsTestFile(name string) bool {
	base := strings.TrimSuffix(name, path.Ext(name))
	return strings.HasSuffix(base, testSuffix)
}

// isYAML reports whether name carries a YAML extension.
func isYAML(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".yaml", ".yml":
		return true
	}
	return false
}
