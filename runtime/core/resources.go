package core

import (
	"context"
	"errors"
)

// ResourceKind identifies the kind of resource a ResourceLoader serves. It lets a
// loader route kinds to different backends (a k8s loader might read env from a
// ConfigMap and templates from another store); the filesystem loader ignores it
// because the id alone maps to a path.
type ResourceKind string

const (
	// ResourceKindEnv is a .env-convention file combined into the runtime environment.
	ResourceKindEnv ResourceKind = "env"
	// ResourceKindTemplate is a text template that may embed {{ }} CEL expressions.
	ResourceKindTemplate ResourceKind = "template"
)

// ErrResourceNotFound is returned by ResourceLoader.Load when no resource exists
// for the given id. Callers decide whether that is fatal: a missing env resource
// is skipped (a missing env file is never a runtime error), while a referenced
// template that is absent is an error.
var ErrResourceNotFound = errors.New("resource: not found")

// ResourceLoader reads resources into the runtime by id. The runtime only ever
// reads: creating, listing, and writing resource files is host-side (done from
// outside the runtime against disk or the orchestrator), so those operations are
// deliberately not on this interface.
//
// The active implementation is chosen at startup: the standalone module resolves
// an id to a file under the config directory; the k8s module is a no-op for now.
type ResourceLoader interface {
	// Load returns the bytes for the resource id of the given kind, or
	// ErrResourceNotFound when it does not exist.
	Load(ctx context.Context, kind ResourceKind, id string) ([]byte, error)
}

// ChangeFunc is invoked by a watching loader when a resource changes, carrying the
// kind and id of the resource that changed so the runtime knows what changed, not
// just that something did.
type ChangeFunc func(kind ResourceKind, id string)

// ResourceWatcher is implemented by loaders that choose to emit change events.
// Implementing it is the opt-in: a loader that does not implement ResourceWatcher
// is simply never watched, so its resources load once per generation and are
// re-read only on an explicit reload. OnChange registers fn to be called whenever
// a resource changes, until ctx is done (which also stops the underlying watch).
type ResourceWatcher interface {
	OnChange(ctx context.Context, fn ChangeFunc) error
}
