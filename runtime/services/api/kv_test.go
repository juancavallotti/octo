package api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
)

// newKVFixture starts a fake with a working KV store and returns the module.
func newKVFixture(t *testing.T) (*Services, *fake, *kvBackend) {
	t.Helper()
	f := newFake(t, fullDiscovery())
	b := newKVBackend()
	b.install(f)
	return newTestServices(t, f, nil), f, b
}

// The version rules are the subtlest part of core.KV and the part a third-party
// implementation is most likely to get wrong, so they are stated end to end.
func TestKVVersionSemantics(t *testing.T) {
	svc, _, _ := newKVFixture(t)
	ctx := t.Context()
	kv := svc.KV()

	// A read of nothing is a miss, not an error.
	if _, ok, err := kv.Get(ctx, core.NamespaceUser, "k"); ok || err != nil {
		t.Fatalf("Get on empty = (ok %v, err %v), want a silent miss", ok, err)
	}

	// Version 0 creates.
	v1, err := kv.Set(ctx, core.NamespaceUser, "k", []byte("one"), 0)
	if err != nil || v1 == 0 {
		t.Fatalf("Set create = (%d, %v), want a positive version", v1, err)
	}

	entry, ok, err := kv.Get(ctx, core.NamespaceUser, "k")
	if err != nil || !ok || string(entry.Value) != "one" || entry.Version != v1 {
		t.Fatalf("Get = (%q, v%d, ok %v, err %v), want the value and version just written",
			entry.Value, entry.Version, ok, err)
	}

	// Creating over an existing key conflicts: 0 means create, not overwrite.
	if _, err := kv.Set(ctx, core.NamespaceUser, "k", []byte("two"), 0); !errors.Is(err, core.ErrVersionConflict) {
		t.Fatalf("Set create over existing = %v, want ErrVersionConflict", err)
	}

	// A stale version conflicts; the current one succeeds.
	if _, err := kv.Set(ctx, core.NamespaceUser, "k", []byte("two"), v1+99); !errors.Is(err, core.ErrVersionConflict) {
		t.Fatalf("Set with a stale version = %v, want ErrVersionConflict", err)
	}
	v2, err := kv.Set(ctx, core.NamespaceUser, "k", []byte("two"), v1)
	if err != nil || v2 <= v1 {
		t.Fatalf("Set update = (%d, %v), want a version above %d", v2, err, v1)
	}
}

// Deleting something that is not there has achieved what the caller asked, so it
// is not an error. Deleting at the wrong version is.
func TestKVDeleteSemantics(t *testing.T) {
	svc, _, _ := newKVFixture(t)
	ctx := t.Context()
	kv := svc.KV()

	if err := kv.Delete(ctx, core.NamespaceUser, "absent", 0); err != nil {
		t.Fatalf("Delete of a missing key = %v, want success", err)
	}
	v, err := kv.Set(ctx, core.NamespaceUser, "k", []byte("v"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := kv.Delete(ctx, core.NamespaceUser, "k", v+99); !errors.Is(err, core.ErrVersionConflict) {
		t.Fatalf("Delete at a stale version = %v, want ErrVersionConflict", err)
	}
	if err := kv.Delete(ctx, core.NamespaceUser, "k", v); err != nil {
		t.Fatalf("Delete at the current version = %v, want success", err)
	}
	if _, ok, _ := kv.Get(ctx, core.NamespaceUser, "k"); ok {
		t.Fatal("the key survived its delete")
	}
}

// Secrets are a view over the same store, routed to the encrypted namespaces —
// so writing a secret and reading the raw KV namespace must not find it.
func TestSecretsRouteToTheSecretNamespaces(t *testing.T) {
	svc, f, _ := newKVFixture(t)
	ctx := t.Context()

	if _, err := svc.Secrets().Set(ctx, core.NamespaceUser, "token", []byte("s3cret"), 0); err != nil {
		t.Fatalf("Secrets Set: %v", err)
	}
	if !strings.Contains(f.last("/entry").path, core.NamespaceUserSecrets) {
		t.Fatalf("secret write went to %s, want the %s namespace",
			f.last("/entry").path, core.NamespaceUserSecrets)
	}
	if _, ok, _ := svc.KV().Get(ctx, core.NamespaceUser, "token"); ok {
		t.Fatal("the secret is readable from the plain namespace")
	}
	entry, ok, err := svc.Secrets().Get(ctx, core.NamespaceUser, "token")
	if err != nil || !ok || string(entry.Value) != "s3cret" {
		t.Fatalf("Secrets Get = (%q, ok %v, err %v)", entry.Value, ok, err)
	}
}

// The wire shape, pinned separately from the semantics: a bug can hide where the
// client and the fake agree but the contract says otherwise.
func TestKVRequestShape(t *testing.T) {
	svc, f, _ := newKVFixture(t)
	if _, err := svc.KV().Set(t.Context(), core.NamespaceSystemVolatile, "a/b", []byte("v"), 7); err != nil {
		// A conflict is fine here; the request shape is what is under test.
		_ = err
	}

	req := f.last("/entry")
	if req.method != "PUT" {
		t.Errorf("method = %s, want PUT", req.method)
	}
	// The namespace is a segment, carrying its full suffixed name.
	if want := "/v1/kv/" + core.NamespaceSystemVolatile + "/entry"; req.path != want {
		t.Errorf("path = %s, want %s", req.path, want)
	}
	// The key is a query parameter: a slash in it must not become a path segment.
	values, err := url.ParseQuery(req.query)
	if err != nil {
		t.Fatal(err)
	}
	if got := values.Get("key"); got != "a/b" {
		t.Errorf("key = %q, want a/b intact", got)
	}
	if got := req.header.Get(headerVersion); got != "7" {
		t.Errorf("%s = %q, want 7", headerVersion, got)
	}
	if got := req.header.Get("Content-Type"); got != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", got)
	}
}

// A server that declared KV and then answers 501 latches it off for the life of
// the process, rather than failing every call forever: this is the drift case a
// once-only discovery cannot otherwise catch.
func TestKVLatchesOffOnNotImplemented(t *testing.T) {
	f := newFake(t, fullDiscovery())
	f.mux.HandleFunc("PUT "+pathKVEntry, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not implemented", http.StatusNotImplemented)
	})
	svc := newTestServices(t, f, nil)
	ctx := t.Context()

	if _, err := svc.KV().Set(ctx, core.NamespaceUser, "k", []byte("v"), 0); !errors.Is(err, core.ErrNoKV) {
		t.Fatalf("Set = %v, want ErrNoKV once the feature latched off", err)
	}
	before := f.count(http.MethodPut, "/entry")
	if _, err := svc.KV().Set(ctx, core.NamespaceUser, "k", []byte("v"), 0); !errors.Is(err, core.ErrNoKV) {
		t.Fatalf("Set after latching = %v, want ErrNoKV", err)
	}
	if after := f.count(http.MethodPut, "/entry"); after != before {
		t.Fatalf("the client called again after latching off (%d then %d)", before, after)
	}
}

// A value over the platform's declared limit fails naming the limit, rather than
// as an opaque rejection from somebody else's proxy.
func TestKVRespectsTheDeclaredValueLimit(t *testing.T) {
	doc := fullDiscovery()
	doc.Features.KV.MaxValueBytes = 4
	f := newFake(t, doc)
	newKVBackend().install(f)
	svc := newTestServices(t, f, nil)

	_, err := svc.KV().Set(t.Context(), core.NamespaceUser, "k", []byte("more than four"), 0)
	if err == nil || !strings.Contains(err.Error(), "limit of 4") {
		t.Fatalf("Set = %v, want a failure naming the declared limit", err)
	}
}

// Resources come back as bytes, and a missing one is ErrResourceNotFound — the
// sentinel the config loader already branches on.
func TestResourcesLoad(t *testing.T) {
	svc, _, b := newKVFixture(t)
	b.resources["env/.env.dev"] = "KEY=value"

	got, err := svc.Resources().Load(t.Context(), core.ResourceKindEnv, ".env.dev")
	if err != nil || string(got) != "KEY=value" {
		t.Fatalf("Load = (%q, %v)", got, err)
	}
	if _, err := svc.Resources().Load(t.Context(), core.ResourceKindEnv, "absent"); !errors.Is(
		err, core.ErrResourceNotFound,
	) {
		t.Fatalf("Load of a missing resource = %v, want ErrResourceNotFound", err)
	}
}

// A resource name may contain a slash, which is why kind and name are query
// parameters rather than path segments.
func TestResourceNamesMayContainSlashes(t *testing.T) {
	svc, _, b := newKVFixture(t)
	b.resources["template/mail/welcome.tmpl"] = "hello"

	got, err := svc.Resources().Load(t.Context(), core.ResourceKindTemplate, "mail/welcome.tmpl")
	if err != nil || string(got) != "hello" {
		t.Fatalf("Load = (%q, %v)", got, err)
	}
}

// Watching means polling somebody else's API forever for a change that arrives as
// a redeploy anyway. Not implementing the interface is how a loader opts out.
func TestResourceLoaderIsNotAWatcher(t *testing.T) {
	svc, _, _ := newKVFixture(t)
	if _, ok := svc.Resources().(core.ResourceWatcher); ok {
		t.Fatal("the api resource loader must not implement core.ResourceWatcher")
	}
}

// A successful write that carries no version is a protocol violation, and it has
// to be reported as one at the moment it happens.
//
// Accepting it looked harmless: the create round-trips, because 0 is what a
// create sends anyway. But the caller then stores 0 and every later update sends
// 0, which means CREATE, so the server answers 409 and the object can never be
// written again — surfacing as a permanent conflict on a key that plainly exists,
// which points at everything except the missing header that caused it.
func TestKVRejectsASuccessWithoutAUsableVersion(t *testing.T) {
	cases := []struct{ name, header string }{
		{"no header at all", ""},
		{"not a number", "banana"},
		{"zero, which means create on the way in", "0"},
		{"negative", "-3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t, fullDiscovery())
			f.mux.HandleFunc("PUT "+pathKVEntry, func(w http.ResponseWriter, _ *http.Request) {
				if tc.header != "" {
					w.Header().Set(headerVersion, tc.header)
				}
				w.WriteHeader(http.StatusOK)
			})
			svc := newTestServices(t, f, nil)

			_, err := svc.KV().Set(t.Context(), core.NamespaceUser, "k", []byte("v"), 0)
			if err == nil {
				t.Fatal("Set accepted a success with no usable version")
			}
			if !strings.Contains(err.Error(), headerVersion) {
				t.Fatalf("err = %v, want it to name the header", err)
			}
		})
	}
}

// And the same on a read: an object that exists has a version, and reporting 0
// for it would send the next write down the create path.
func TestKVRejectsAReadWithoutAUsableVersion(t *testing.T) {
	f := newFake(t, fullDiscovery())
	f.mux.HandleFunc("GET "+pathKVEntry, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("a value with no version"))
	})
	svc := newTestServices(t, f, nil)

	if _, _, err := svc.KV().Get(t.Context(), core.NamespaceUser, "k"); err == nil {
		t.Fatal("Get accepted a hit with no version")
	}
}
