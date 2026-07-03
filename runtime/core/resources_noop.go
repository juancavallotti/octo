package core

import "context"

// NoopResourceLoader is the fallback ResourceLoader used when none is wired: every
// Load reports the resource missing. It does not implement ResourceWatcher, so it
// is never watched. It is also what the byte-parse config entrypoint uses, where
// declared env resources simply never load — which is correct, since a missing env
// resource is not a runtime error.
type NoopResourceLoader struct{}

// Load always reports the resource missing.
func (NoopResourceLoader) Load(context.Context, ResourceKind, string) ([]byte, error) {
	return nil, ErrResourceNotFound
}
