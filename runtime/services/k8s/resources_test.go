package k8s

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
)

func TestHTTPResourceLoaderLoad(t *testing.T) {
	var gotPath, gotAuth, gotKind, gotName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotKind = r.URL.Query().Get("kind")
		gotName = r.URL.Query().Get("name")
		if gotKind == string(core.ResourceKindEnv) && gotName == ".env.dev" {
			_, _ = w.Write([]byte("GREETING=hi"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	loader := newHTTPResourceLoader(srv.URL, "snap-1", "tok")
	defer loader.close()

	t.Run("returns the frozen bytes and shapes the request", func(t *testing.T) {
		got, err := loader.Load(context.Background(), core.ResourceKindEnv, ".env.dev")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if string(got) != "GREETING=hi" {
			t.Errorf("content = %q, want the frozen bytes", got)
		}
		if gotPath != "/snapshots/snap-1/resources/content" {
			t.Errorf("path = %q, want the snapshot content path", gotPath)
		}
		if gotKind != string(core.ResourceKindEnv) || gotName != ".env.dev" {
			t.Errorf("kind/name = %q/%q, want env/.env.dev", gotKind, gotName)
		}
		if gotAuth != "Bearer tok" {
			t.Errorf("Authorization = %q, want Bearer tok", gotAuth)
		}
	})

	t.Run("maps 404 to ErrResourceNotFound", func(t *testing.T) {
		_, err := loader.Load(context.Background(), core.ResourceKindTemplate, "missing.tmpl")
		if !errors.Is(err, core.ErrResourceNotFound) {
			t.Errorf("error = %v, want ErrResourceNotFound", err)
		}
	})

	t.Run("carries path-like names with slashes as a query param", func(t *testing.T) {
		_, _ = loader.Load(context.Background(), core.ResourceKindTemplate, "templates/mail/welcome.tmpl")
		if gotName != "templates/mail/welcome.tmpl" {
			t.Errorf("name = %q, want the slash-bearing name intact", gotName)
		}
		if gotPath != "/snapshots/snap-1/resources/content" {
			t.Errorf("path = %q, want the name to stay out of the path", gotPath)
		}
	})
}

func TestHTTPResourceLoaderNoToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	loader := newHTTPResourceLoader(srv.URL, "snap-1", "")
	defer loader.close()
	_, _ = loader.Load(context.Background(), core.ResourceKindEnv, "x")
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want none when no token is configured", gotAuth)
	}
}
