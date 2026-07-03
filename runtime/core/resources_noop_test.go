package core

import (
	"context"
	"errors"
	"testing"
)

func TestNoopResourceLoaderLoadMissing(t *testing.T) {
	_, err := NoopResourceLoader{}.Load(context.Background(), ResourceKindEnv, ".env.dev")
	if !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("Load err = %v, want ErrResourceNotFound", err)
	}
}

// The Noop loader must not satisfy ResourceWatcher: not implementing it is how a
// loader opts out of change notification.
func TestNoopResourceLoaderIsNotWatcher(t *testing.T) {
	var loader ResourceLoader = NoopResourceLoader{}
	if _, ok := loader.(ResourceWatcher); ok {
		t.Fatal("NoopResourceLoader must not implement ResourceWatcher")
	}
}
