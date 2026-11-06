// Package environment is the orchestrator feature module for environments: named,
// isolated slices of the platform, each running in a Kubernetes namespace of its
// own. Creating one provisions its namespace; deleting one removes it, and is
// refused while anything still runs there.
//
// The default environment is not stored. It is the release namespace, named
// Default, and every environment-scoped column spells it NULL.
package environment

import (
	"regexp"
	"time"
)

// Default is the name of the default environment: the release namespace, which
// every installation has and none can delete.
const Default = "octo"

// Environment is one environment and the namespace it runs in.
type Environment struct {
	Name        string
	Namespace   string
	DisplayName string
	Description string
	// CreatedAt and CreatedBy are zero for the default environment, which was
	// never created by anyone.
	CreatedAt time.Time
	CreatedBy *string
	// IsDefault marks the synthesized default environment.
	IsDefault bool
}

// Input is what a caller supplies to create an environment. Namespace is
// optional and defaults to "octo-" + Name.
type Input struct {
	Name        string
	Namespace   string
	DisplayName string
	Description string
}

// Patch is what may change after creation. The name and namespace never do: the
// name is what scoped rows and tokens refer to, and the namespace is where
// workloads already run.
type Patch struct {
	DisplayName *string
	Description *string
}

// maxNameLen bounds an environment name so that the default namespace,
// "octo-" + name, stays well inside the 63-character DNS label limit with room
// for the resource names built from it.
const maxNameLen = 30

// nameRe is a DNS-1123 label: the name becomes part of a namespace and a label
// value, and both take exactly this.
var nameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// namespaceRe is a DNS-1123 label of at most 63 characters, which is what
// Kubernetes accepts as a namespace name.
var namespaceRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)

// ValidName reports whether name can name an environment. Default is not one:
// it already exists.
func ValidName(name string) bool {
	return len(name) <= maxNameLen && nameRe.MatchString(name) && name != Default
}

// validNamespace reports whether namespace is a usable Kubernetes namespace.
// The kube-* namespaces belong to the cluster and "default" to whoever uses it.
func validNamespace(namespace string) bool {
	if !namespaceRe.MatchString(namespace) || namespace == "default" {
		return false
	}
	return len(namespace) < 5 || namespace[:5] != "kube-"
}
