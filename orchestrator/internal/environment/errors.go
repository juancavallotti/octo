package environment

import "errors"

var (
	// ErrInvalidName is returned for a name that is not a DNS label of at most 30
	// characters, or that is the default environment's.
	ErrInvalidName = errors.New("invalid environment name")
	// ErrInvalidNamespace is returned for a namespace Kubernetes would refuse, or
	// one that belongs to the cluster.
	ErrInvalidNamespace = errors.New("invalid namespace")
	// ErrExists is returned when the name is already an environment.
	ErrExists = errors.New("environment already exists")
	// ErrNamespaceTaken is returned when the namespace already belongs to another
	// environment, to the release, or to something outside octo.
	ErrNamespaceTaken = errors.New("namespace is already in use")
	// ErrNotFound is returned when no environment has the name.
	ErrNotFound = errors.New("environment not found")
	// ErrDefault is returned for a change to the default environment, which has
	// nothing stored to change and cannot be deleted.
	ErrDefault = errors.New("the default environment cannot be changed")
	// ErrInUse is returned when deleting an environment whose namespace still
	// runs something the orchestrator manages.
	ErrInUse = errors.New("environment still has workloads")
	// ErrUnavailable is returned when this installation cannot provision
	// namespaces: there is no cluster, or the chart has not granted the rules.
	ErrUnavailable = errors.New("environments unavailable")
)
