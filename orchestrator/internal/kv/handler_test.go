package kv

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeStore is an in-memory Store for handler tests, keyed by namespace+key.
type fakeStore struct {
	rows    map[string][]byte
	version map[string]int64
	setErr  error
}

func newFakeStore() *fakeStore {
	return &fakeStore{rows: map[string][]byte{}, version: map[string]int64{}}
}

func (f *fakeStore) Get(_ context.Context, _, namespace, key string) ([]byte, int64, bool, error) {
	k := namespace + "/" + key
	v, ok := f.rows[k]
	return v, f.version[k], ok, nil
}

func (f *fakeStore) Set(_ context.Context, _, namespace, key string, value []byte, _ int64) (int64, error) {
	if f.setErr != nil {
		return 0, f.setErr
	}
	k := namespace + "/" + key
	f.rows[k] = value
	f.version[k]++
	return f.version[k], nil
}

func (f *fakeStore) Delete(_ context.Context, _, namespace, key string, _ int64) error {
	delete(f.rows, namespace+"/"+key)
	return nil
}

func newTestServer(t *testing.T, store Store) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	NewHandler(store).Register(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func do(t *testing.T, method, url, version, body string) *http.Response {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if version != "" {
		req.Header.Set(headerVersion, version)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, url, err)
	}
	return resp
}

func TestHandlerPutThenGet(t *testing.T) {
	ts := newTestServer(t, newFakeStore())
	base := ts.URL + "/deployments/dep-1/kv/user/my-key"

	put := do(t, http.MethodPut, base, "0", "hello")
	defer put.Body.Close()
	if put.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", put.StatusCode)
	}
	if put.Header.Get(headerVersion) != "1" {
		t.Fatalf("PUT version header = %q, want 1", put.Header.Get(headerVersion))
	}

	get := do(t, http.MethodGet, base, "", "")
	defer get.Body.Close()
	if get.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", get.StatusCode)
	}
	if get.Header.Get(headerVersion) != "1" {
		t.Fatalf("GET version header = %q, want 1", get.Header.Get(headerVersion))
	}
	body, _ := io.ReadAll(get.Body)
	if string(body) != "hello" {
		t.Fatalf("GET body = %q, want \"hello\"", body)
	}
}

func TestHandlerGetMissing(t *testing.T) {
	ts := newTestServer(t, newFakeStore())
	resp := do(t, http.MethodGet, ts.URL+"/deployments/dep-1/kv/user/absent", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHandlerConflictIs409(t *testing.T) {
	store := newFakeStore()
	store.setErr = ErrVersionConflict
	ts := newTestServer(t, store)
	resp := do(t, http.MethodPut, ts.URL+"/deployments/dep-1/kv/user/k", "9", "x")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
}

func TestHandlerEncryptionDisabledIs503(t *testing.T) {
	store := newFakeStore()
	store.setErr = ErrEncryptionDisabled
	ts := newTestServer(t, store)
	resp := do(t, http.MethodPut, ts.URL+"/deployments/dep-1/kv/system_secrets/k", "0", "x")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}
