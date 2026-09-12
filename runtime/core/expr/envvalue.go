package expr

import (
	"sync"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// Environment variables whose value is produced on use rather than read once.
//
// `env.NAME` is an ordinary lookup in a map resolved when the config was loaded,
// and that is right for every variable an operator sets: it cannot change under a
// running process. One kind of value is not like that — a credential this runtime
// keeps valid on the integration's behalf, which is a different string an hour
// from now and has to be the current one at the moment a request is built.
//
// Rather than teach flow authors a second way to read a value, such a variable is
// still spelled `env.NAME`. What differs is underneath: a provider registered for
// that name is asked when the lookup happens. A runtime with no provider for the
// name does the plain lookup, so on a platform where somebody simply sets the
// variable it behaves like any other — which is what lets one definition load in
// the editor, run under dolphin, and work in the cluster.
//
// The resolution is in the *map*, not in the value it returns. A value that
// resolved itself later would be a type of its own wherever it landed, and CEL
// dispatches operators on the value it is handed: `"Bearer " + env.NAME` would
// find no overload for it, and every string operation would have to be
// reimplemented on it. Returning a real string from the lookup means the rest of
// the language never learns this happened.

// Env is the resolved environment as an expression sees it.
type Env = traits.Mapper

// envProviders holds the registered dynamic values by variable name. Written
// while a services provider is being constructed and read on every lookup, so it
// is guarded rather than assumed immutable after startup.
var envProviders sync.Map // name -> func() string

// RegisterEnvValue makes `env.NAME` resolve through fn rather than through the
// loaded environment, for every expression evaluated in this process.
//
// For a value this runtime maintains and an operator cannot: a token it renews,
// say. Registering one is a services provider's business — that is the layer
// which knows whether this process has such a thing — and a build without that
// provider registers nothing.
//
// fn is called during expression evaluation, so it must answer from something
// already held. A provider that needs the network should refresh out of band and
// hand back what it has.
func RegisterEnvValue(name string, fn func() string) {
	envProviders.Store(name, fn)
}

// envMap is the environment CEL indexes: the loaded values, with the registered
// names answered by their provider at the moment they are read.
//
// It embeds the map CEL would otherwise have used, so everything that is not a
// lookup — iteration, size, conversion, equality — stays that map's behaviour and
// cannot drift from it.
type envMap struct {
	traits.Mapper
}

// resolved returns the provider's current value for name, if there is one.
//
//nolint:ireturn // ref.Val is cel-go's currency; every lookup here deals in it.
func resolved(name ref.Val) (ref.Val, bool) {
	key, ok := name.(types.String)
	if !ok {
		return nil, false
	}
	fn, ok := envProviders.Load(string(key))
	if !ok {
		return nil, false
	}
	return types.String(fn.(func() string)()), true
}

// Find answers a lookup, preferring a provider over the loaded value: the
// provider is the authority on a value it maintains, and a copy sitting in the
// environment is exactly what it is there to replace.
//
//nolint:ireturn // implementing traits.Mapper.
func (e envMap) Find(key ref.Val) (ref.Val, bool) {
	if value, ok := resolved(key); ok {
		return value, true
	}
	return e.Mapper.Find(key)
}

// Get is Find for the indexing operator, which is how `env.NAME` arrives.
//
//nolint:ireturn // implementing traits.Mapper.
func (e envMap) Get(key ref.Val) ref.Val {
	if value, ok := resolved(key); ok {
		return value
	}
	return e.Mapper.Get(key)
}

// Contains reports a registered name as present whether or not the environment
// holds one, so `has(env.NAME)` answers about the provider rather than about
// whether somebody also set a variable of that name.
//
//nolint:ireturn // implementing traits.Mapper.
func (e envMap) Contains(key ref.Val) ref.Val {
	if _, ok := resolved(key); ok {
		return types.True
	}
	return e.Mapper.Contains(key)
}

// EnvActivation materializes a resolved env map into the form CEL expects once at
// build time, so it can be shared across every message a block processes. A nil or
// empty env yields a non-nil empty map, keeping env.NAME a missing-key error
// rather than a null-deref.
//
// The result answers a name some services provider maintains when it is read
// rather than now — the map is built once and shared, and a credential read at
// that moment would be the same expired string for the life of the process.
//
// unexported, so nothing outside this package depends on how it resolves.
//
//nolint:ireturn // Env is the CEL map interface; the concrete type stays
func EnvActivation(env map[string]string) Env {
	out := make(map[string]any, len(env))
	for k, v := range env {
		out[k] = v
	}
	return envMap{Mapper: types.NewStringInterfaceMap(types.DefaultTypeAdapter, out)}
}
