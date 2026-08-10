package kv

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newObjectServer(t *testing.T, store Store) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	NewObjectHandler(store).Register(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestObjectPutGetList(t *testing.T) {
	store := newFakeStore()
	ts := newObjectServer(t, store)

	// Create two keys via the JSON facade.
	for _, k := range []string{"beta", "alpha"} {
		resp := do(t, http.MethodPut, ts.URL+"/deployments/dep-1/objects/"+k, "",
			`{"value":"`+k+`","version":0}`)
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("PUT %s status = %d, want 200 (%s)", k, resp.StatusCode, body)
		}
	}

	// The facade writes to the user namespace only.
	if _, ok := store.rows["user/alpha"]; !ok {
		t.Fatal("alpha not stored under the user namespace")
	}

	// Get returns the value with a utf8 encoding.
	get := do(t, http.MethodGet, ts.URL+"/deployments/dep-1/objects/alpha", "", "")
	defer func() { _ = get.Body.Close() }()
	if get.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", get.StatusCode)
	}
	var val objectValue
	if err := json.NewDecoder(get.Body).Decode(&val); err != nil {
		t.Fatalf("decode value: %v", err)
	}
	if val.Value != "alpha" || val.Encoding != "utf8" || val.Version != 1 {
		t.Fatalf("got %+v, want value=alpha encoding=utf8 version=1", val)
	}

	// List returns both keys, ordered by key.
	list := do(t, http.MethodGet, ts.URL+"/deployments/dep-1/objects", "", "")
	defer func() { _ = list.Body.Close() }()
	var listed struct {
		Items []Entry `json:"items"`
	}
	if err := json.NewDecoder(list.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Items) != 2 || listed.Items[0].Key != "alpha" || listed.Items[1].Key != "beta" {
		t.Fatalf("list = %+v, want [alpha, beta]", listed.Items)
	}
}

func TestObjectListEmptyIsArray(t *testing.T) {
	ts := newObjectServer(t, newFakeStore())
	resp := do(t, http.MethodGet, ts.URL+"/deployments/dep-1/objects", "", "")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if got := string(body); got != "{\"items\":[]}\n" {
		t.Fatalf("empty list = %q, want an empty items array", got)
	}
}

func TestObjectGetMissingIs404(t *testing.T) {
	ts := newObjectServer(t, newFakeStore())
	resp := do(t, http.MethodGet, ts.URL+"/deployments/dep-1/objects/absent", "", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestObjectPutConflictIs409(t *testing.T) {
	store := newFakeStore()
	store.setErr = ErrVersionConflict
	ts := newObjectServer(t, store)
	resp := do(t, http.MethodPut, ts.URL+"/deployments/dep-1/objects/k", "",
		`{"value":"x","version":9}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
}

func TestObjectDeleteUsesVersionQuery(t *testing.T) {
	store := newFakeStore()
	ts := newObjectServer(t, store)
	if _, err := store.Set(context.Background(), "dep-1", "user", "k", []byte("v"), 0); err != nil {
		t.Fatalf("seed: %v", err)
	}
	resp := do(t, http.MethodDelete, ts.URL+"/deployments/dep-1/objects/k?version=1", "", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if _, ok := store.rows["user/k"]; ok {
		t.Fatal("key present after delete")
	}
}

func TestObjectNamespaceParamRoutesAndListsNamespaces(t *testing.T) {
	store := newFakeStore()
	ts := newObjectServer(t, store)

	// A write naming a non-default namespace lands there, not in "user".
	resp := do(t, http.MethodPut, ts.URL+"/deployments/dep-1/objects/k?namespace=system", "",
		`{"value":"v","version":0}`)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", resp.StatusCode)
	}
	if _, ok := store.rows["system/k"]; !ok {
		t.Fatal("key not stored under the system namespace")
	}

	// The namespaces endpoint advertises user (always) plus every populated one,
	// including secret namespaces (the browser can list/clean them up).
	store.rows["user_secrets/s"] = []byte("ciphertext")
	list := do(t, http.MethodGet, ts.URL+"/deployments/dep-1/namespaces", "", "")
	defer func() { _ = list.Body.Close() }()
	var listed struct {
		Items []string `json:"items"`
	}
	if err := json.NewDecoder(list.Body).Decode(&listed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listed.Items) != 3 || listed.Items[0] != "user" ||
		listed.Items[1] != "system" || listed.Items[2] != "user_secrets" {
		t.Fatalf("namespaces = %v, want [user system user_secrets]", listed.Items)
	}
}

// Secret namespaces can be listed and cleaned up but never read or written through
// the object facade.
func TestObjectSecretNamespaceListAndDeleteButNotReadOrWrite(t *testing.T) {
	store := newFakeStore()
	store.rows["user_secrets/oauth"] = []byte("ciphertext")
	store.version["user_secrets/oauth"] = 1
	ts := newObjectServer(t, store)

	// Listing a secret namespace returns key metadata (not the value).
	list := do(t, http.MethodGet, ts.URL+"/deployments/dep-1/objects?namespace=user_secrets", "", "")
	defer func() { _ = list.Body.Close() }()
	var listed struct {
		Items []Entry `json:"items"`
	}
	if err := json.NewDecoder(list.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0].Key != "oauth" {
		t.Fatalf("list = %+v, want [oauth]", listed.Items)
	}

	// Reading and writing a secret value are both refused.
	get := do(t, http.MethodGet, ts.URL+"/deployments/dep-1/objects/oauth?namespace=user_secrets", "", "")
	_ = get.Body.Close()
	if get.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET status = %d, want 400", get.StatusCode)
	}
	put := do(t, http.MethodPut, ts.URL+"/deployments/dep-1/objects/oauth?namespace=user_secrets", "",
		`{"value":"x","version":1}`)
	_ = put.Body.Close()
	if put.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT status = %d, want 400", put.StatusCode)
	}

	// Deleting a secret key (cleanup) is allowed.
	del := do(t, http.MethodDelete, ts.URL+"/deployments/dep-1/objects/oauth?namespace=user_secrets&version=1", "", "")
	_ = del.Body.Close()
	if del.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", del.StatusCode)
	}
	if _, ok := store.rows["user_secrets/oauth"]; ok {
		t.Fatal("secret key present after delete")
	}
}

func TestObjectBinaryRoundTripsAsBase64(t *testing.T) {
	store := newFakeStore()
	ts := newObjectServer(t, store)
	store.rows["user/bin"] = []byte{0xff, 0xfe, 0x00}
	store.version["user/bin"] = 3

	get := do(t, http.MethodGet, ts.URL+"/deployments/dep-1/objects/bin", "", "")
	defer func() { _ = get.Body.Close() }()
	var val objectValue
	if err := json.NewDecoder(get.Body).Decode(&val); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if val.Encoding != "base64" || val.Value != "//4A" {
		t.Fatalf("got encoding=%q value=%q, want base64 //4A", val.Encoding, val.Value)
	}
}
