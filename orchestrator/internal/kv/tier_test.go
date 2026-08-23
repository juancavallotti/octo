package kv

import (
	"context"
	"errors"
	"testing"
)

// countingRepo records which calls reached it, so a test can assert *which* store a
// namespace was routed to rather than only that the call succeeded.
type countingRepo struct {
	name        string
	gets        int
	writes      int
	deletes     int
	lists       int
	namespaces  []string
	deployments int
	err         error
}

func (r *countingRepo) Get(context.Context, string, string, string) ([]byte, int64, bool, error) {
	r.gets++
	return []byte(r.name), 1, true, r.err
}

func (r *countingRepo) Write(context.Context, string, string, string, []byte, int64) (int64, error) {
	r.writes++
	return 1, r.err
}

func (r *countingRepo) Delete(context.Context, string, string, string, int64) error {
	r.deletes++
	return r.err
}

func (r *countingRepo) List(context.Context, string, string) ([]Entry, error) {
	r.lists++
	return []Entry{{Key: r.name}}, r.err
}

func (r *countingRepo) ListNamespaces(context.Context, string) ([]string, error) {
	return r.namespaces, r.err
}

func (r *countingRepo) DeleteByDeployment(context.Context, string) error {
	r.deployments++
	return r.err
}

// A namespace picks its store. Nothing above the service knows there are two, so
// this is where the routing has to be pinned.
func TestServiceRoutesByTier(t *testing.T) {
	cases := []struct {
		namespace string
		volatile  bool
	}{
		{"user", false},
		{"system", false},
		{"user_secrets", false},
		{"user_volatile", true},
		{"system_volatile", true},
	}
	for _, tc := range cases {
		persistent := &countingRepo{name: "postgres"}
		volatile := &countingRepo{name: "redis"}
		svc := NewService(persistent, volatile, testCipher(t))
		ctx := context.Background()

		if _, _, _, err := svc.Get(ctx, "dep-1", tc.namespace, "k"); err != nil && !isSecret(tc.namespace) {
			t.Fatalf("Get(%q): %v", tc.namespace, err)
		}
		if _, err := svc.Set(ctx, "dep-1", tc.namespace, "k", []byte("v"), 0); err != nil {
			t.Fatalf("Set(%q): %v", tc.namespace, err)
		}
		if err := svc.Delete(ctx, "dep-1", tc.namespace, "k", 0); err != nil {
			t.Fatalf("Delete(%q): %v", tc.namespace, err)
		}
		if _, err := svc.List(ctx, "dep-1", tc.namespace); err != nil {
			t.Fatalf("List(%q): %v", tc.namespace, err)
		}

		hit, missed := persistent, volatile
		if tc.volatile {
			hit, missed = volatile, persistent
		}
		// gets included: a Service.Get that returned before reaching a repository
		// would otherwise leave this green while read routing was broken.
		if hit.gets == 0 || hit.writes == 0 || hit.deletes == 0 || hit.lists == 0 {
			t.Errorf("%q did not reach the %s store (gets=%d writes=%d deletes=%d lists=%d)",
				tc.namespace, hit.name, hit.gets, hit.writes, hit.deletes, hit.lists)
		}
		if missed.gets+missed.writes+missed.deletes+missed.lists != 0 {
			t.Errorf("%q also reached the %s store", tc.namespace, missed.name)
		}
	}
}

// Write is where the fallback matters most: refusing a volatile write because an
// optional dependency is absent would take the platform down over the one tier
// whose promise is that losing it is survivable.
func TestServiceFallsBackWhenNoVolatileStore(t *testing.T) {
	for _, volatile := range []repository{nil, (*RedisRepo)(nil)} {
		persistent := &countingRepo{name: "postgres"}
		svc := NewService(persistent, volatile, testCipher(t))

		if _, err := svc.Set(context.Background(), "dep-1", "user_volatile", "k", []byte("v"), 0); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if persistent.writes != 1 {
			t.Errorf("a volatile write with no volatile store should land in the persistent one")
		}
	}
}

// A namespace lives in one store or the other, so a browser shown only one store's
// listing would be shown half of what the deployment has.
func TestListNamespacesUnionsBothTiers(t *testing.T) {
	persistent := &countingRepo{name: "postgres", namespaces: []string{"user", "system"}}
	volatile := &countingRepo{name: "redis", namespaces: []string{"user_volatile", "user"}}
	svc := NewService(persistent, volatile, testCipher(t))

	got, err := svc.ListNamespaces(context.Background(), "dep-1")
	if err != nil {
		t.Fatalf("ListNamespaces: %v", err)
	}
	want := []string{"user", "system", "user_volatile"} // "user" appears once
	if len(got) != len(want) {
		t.Fatalf("namespaces = %v, want %v", got, want)
	}
	for i, ns := range want {
		if got[i] != ns {
			t.Fatalf("namespaces = %v, want %v", got, want)
		}
	}
}

// Undeploy cleanup must try both stores even when the first fails: they are
// independent, and skipping the second would leave orphans nothing goes back for.
func TestDeleteByDeploymentSweepsBothTiersEvenOnError(t *testing.T) {
	boom := errors.New("postgres is down")
	persistent := &countingRepo{name: "postgres", err: boom}
	volatile := &countingRepo{name: "redis"}
	svc := NewService(persistent, volatile, testCipher(t))

	err := svc.DeleteByDeployment(context.Background(), "dep-1")
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want it to report the persistent failure", err)
	}
	if volatile.deployments != 1 {
		t.Error("the volatile store was not swept after the persistent one failed")
	}
}

// valueRepo is a repository implemented as a struct value rather than a pointer.
// reflect's IsNil panics on a kind that cannot be nil, so the constructor's
// typed-nil normalization has to check the kind before asking.
type valueRepo struct{}

func (valueRepo) Get(context.Context, string, string, string) ([]byte, int64, bool, error) {
	return nil, 0, false, nil
}
func (valueRepo) Write(context.Context, string, string, string, []byte, int64) (int64, error) {
	return 1, nil
}
func (valueRepo) Delete(context.Context, string, string, string, int64) error { return nil }
func (valueRepo) List(context.Context, string, string) ([]Entry, error)       { return nil, nil }
func (valueRepo) ListNamespaces(context.Context, string) ([]string, error)    { return nil, nil }
func (valueRepo) DeleteByDeployment(context.Context, string) error            { return nil }

func TestNewServiceAcceptsANonPointerRepo(t *testing.T) {
	// Must not panic, and must not be mistaken for an absent store.
	svc := NewService(&countingRepo{name: "postgres"}, valueRepo{}, testCipher(t))
	if svc.volatile == nil {
		t.Error("a struct-valued repository was normalized away as if it were nil")
	}
}

// A colon is the volatile key layout's delimiter, and only the object key may
// contain one. Without this, namespace "a:b" + key "k" and namespace "a" + key
// "b:k" produce the same Redis key: two callers who believe they are in separate
// keyspaces sharing one, with each other's versions — and ListNamespaces reporting
// only the part before the first colon, so the aliasing would not even be visible.
func TestVolatileNamespacesRejectColons(t *testing.T) {
	repo := NewRedisRepo(nil)
	if repo != nil {
		t.Fatal("NewRedisRepo(nil) should be nil")
	}

	// Checked before the client is touched, so a nil-client repo still exercises it.
	bad := &RedisRepo{}
	ctx := context.Background()

	if _, _, _, err := bad.Get(ctx, "dep-1", "user:evil", "k"); !errors.Is(err, ErrInvalidNamespace) {
		t.Errorf("Get = %v, want ErrInvalidNamespace", err)
	}
	if _, err := bad.Write(ctx, "dep-1", "user:evil", "k", []byte("v"), 0); !errors.Is(err, ErrInvalidNamespace) {
		t.Errorf("Write = %v, want ErrInvalidNamespace", err)
	}
	if err := bad.Delete(ctx, "dep-1", "user:evil", "k", 0); !errors.Is(err, ErrInvalidNamespace) {
		t.Errorf("Delete = %v, want ErrInvalidNamespace", err)
	}
	if _, err := bad.List(ctx, "dep-1", "user:evil"); !errors.Is(err, ErrInvalidNamespace) {
		t.Errorf("List = %v, want ErrInvalidNamespace", err)
	}

	// The aliasing this prevents, spelled out: these two would otherwise be the
	// same Redis key.
	if keyOf("d", "a:b", "k") != keyOf("d", "a", "b:k") {
		t.Fatal("the premise changed; this test is no longer describing the hazard")
	}
}
