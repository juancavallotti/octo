package k8s

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/core"
)

// kvServer is a minimal in-memory stand-in for the orchestrator store API, enough
// to exercise the client's request shaping and status handling.
type kvServer struct {
	value   []byte
	version int64
	exists  bool
	lastReq *http.Request
}

func (s *kvServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.lastReq = r
		switch r.Method {
		case http.MethodGet:
			if !s.exists {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set(headerVersion, strconv.FormatInt(s.version, 10))
			_, _ = w.Write(s.value)
		case http.MethodPut:
			expected, _ := strconv.ParseInt(r.Header.Get(headerVersion), 10, 64)
			current := int64(0)
			if s.exists {
				current = s.version
			}
			if expected != current {
				w.WriteHeader(http.StatusConflict)
				return
			}
			body, _ := io.ReadAll(r.Body)
			s.value = body
			s.version = current + 1
			s.exists = true
			w.Header().Set(headerVersion, strconv.FormatInt(s.version, 10))
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			s.exists = false
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

func newTestClient(t *testing.T, srv *kvServer) *httpStore {
	t.Helper()
	ts := httptest.NewServer(srv.handler())
	t.Cleanup(ts.Close)
	return newHTTPStore(ts.URL, "dep-123", "")
}

func TestKVGetMissing(t *testing.T) {
	c := newTestClient(t, &kvServer{exists: false})
	_, ok, err := c.Get(context.Background(), core.NamespaceUser, "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for a missing key")
	}
}

func TestKVSetThenGet(t *testing.T) {
	srv := &kvServer{}
	c := newTestClient(t, srv)
	ctx := context.Background()

	v, err := c.Set(ctx, core.NamespaceUser, "k", []byte("hello"), 0)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v != 1 {
		t.Fatalf("version = %d, want 1", v)
	}

	entry, ok, err := c.Get(ctx, core.NamespaceUser, "k")
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if string(entry.Value) != "hello" || entry.Version != 1 {
		t.Fatalf("Get = %q v%d, want \"hello\" v1", entry.Value, entry.Version)
	}
}

func TestSecretsRouteToEncryptedNamespace(t *testing.T) {
	// The SecretStore wrapper routes to the KV /kv endpoint under the secret
	// namespace, so the orchestrator encrypts it.
	srv := &kvServer{}
	ts := httptest.NewServer(srv.handler())
	t.Cleanup(ts.Close)
	store := newHTTPStore(ts.URL, "dep-123", "")
	secrets := core.NewSecretStore(store)
	if _, err := secrets.Set(context.Background(), core.NamespaceSystem, "token", []byte("s"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !strings.Contains(srv.lastReq.URL.Path, "/kv/"+core.NamespaceSystemSecrets+"/") {
		t.Fatalf("path %q does not target the %q namespace", srv.lastReq.URL.Path, core.NamespaceSystemSecrets)
	}
}

func TestKVConflictMapsToError(t *testing.T) {
	// Server already holds version 1; a write with expectedVersion 0 conflicts.
	srv := &kvServer{exists: true, version: 1, value: []byte("v")}
	c := newTestClient(t, srv)
	if _, err := c.Set(context.Background(), core.NamespaceUser, "k", []byte("x"), 0); !errors.Is(err, core.ErrVersionConflict) {
		t.Fatalf("Set conflict: err = %v, want ErrVersionConflict", err)
	}
}

func TestKVSendsVersionAndNamespaceInPath(t *testing.T) {
	srv := &kvServer{exists: true, version: 7, value: []byte("v")}
	c := newTestClient(t, srv)
	if _, err := c.Set(context.Background(), core.NamespaceUser, "my/key", []byte("x"), 7); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := srv.lastReq.Header.Get(headerVersion); got != "7" {
		t.Fatalf("X-Object-Version = %q, want 7", got)
	}
	// The deployment id, namespace, and (escaped) key all appear in the path.
	path := srv.lastReq.URL.Path
	for _, want := range []string{"dep-123", core.NamespaceUser, "my/key"} {
		if !strings.Contains(path, want) {
			t.Fatalf("path %q missing %q", path, want)
		}
	}
}

func TestKVDelete(t *testing.T) {
	srv := &kvServer{exists: true, version: 1, value: []byte("v")}
	c := newTestClient(t, srv)
	if err := c.Delete(context.Background(), core.NamespaceUser, "k", 1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if srv.exists {
		t.Fatal("key still present after delete")
	}
}

func TestKVTokenAuth(t *testing.T) {
	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(ts.Close)
	c := newHTTPStore(ts.URL, "dep-123", "tok-abc")
	if _, _, err := c.Get(context.Background(), core.NamespaceUser, "k"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if gotAuth != "Bearer tok-abc" {
		t.Fatalf("Authorization = %q, want \"Bearer tok-abc\"", gotAuth)
	}
}
