package api

import (
	"net/http"
	"strconv"
	"sync"
)

// kvBackend is the fake's key-value store: a versioned map with the same
// optimistic-concurrency rules the contract requires. It is what lets a test say
// "a conflicting write returns ErrVersionConflict" rather than "the client sent a
// PUT with this header".
type kvBackend struct {
	mu      sync.Mutex
	entries map[string]kvRow
	// resources is what the resource routes serve, keyed "kind/name".
	resources map[string]string
}

type kvRow struct {
	value   []byte
	version int64
}

func newKVBackend() *kvBackend {
	return &kvBackend{entries: map[string]kvRow{}, resources: map[string]string{}}
}

// install registers the KV and resource routes on the fake.
func (b *kvBackend) install(f *fake) {
	f.mux.HandleFunc("GET "+servePath(pathKVEntry), b.get)
	f.mux.HandleFunc("PUT "+servePath(pathKVEntry), b.set)
	f.mux.HandleFunc("DELETE "+servePath(pathKVEntry), b.delete)
	f.mux.HandleFunc("GET /v1/resources/content", b.resource)
}

func (b *kvBackend) key(r *http.Request) string {
	return r.PathValue("namespace") + "\x00" + r.URL.Query().Get("key")
}

func (b *kvBackend) get(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	row, ok := b.entries[b.key(r)]
	b.mu.Unlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set(headerVersion, strconv.FormatInt(row.version, 10))
	_, _ = w.Write(row.value)
}

// set applies the version rules the contract states: 0 creates and fails if the
// key already exists; a positive value must match.
func (b *kvBackend) set(w http.ResponseWriter, r *http.Request) {
	expected := parseVersion(r.Header.Get(headerVersion))
	body := readAll(r)

	b.mu.Lock()
	defer b.mu.Unlock()
	key := b.key(r)
	row, exists := b.entries[key]
	if (expected == 0 && exists) || (expected != 0 && (!exists || row.version != expected)) {
		http.Error(w, "version conflict", http.StatusConflict)
		return
	}
	row.version++
	row.value = body
	b.entries[key] = row
	w.Header().Set(headerVersion, strconv.FormatInt(row.version, 10))
	w.WriteHeader(http.StatusOK)
}

func (b *kvBackend) delete(w http.ResponseWriter, r *http.Request) {
	expected := parseVersion(r.Header.Get(headerVersion))

	b.mu.Lock()
	defer b.mu.Unlock()
	key := b.key(r)
	row, exists := b.entries[key]
	if !exists {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if expected != 0 && row.version != expected {
		http.Error(w, "version conflict", http.StatusConflict)
		return
	}
	delete(b.entries, key)
	w.WriteHeader(http.StatusNoContent)
}

func (b *kvBackend) resource(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	body, ok := b.resources[r.URL.Query().Get("kind")+"/"+r.URL.Query().Get("name")]
	b.mu.Unlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	_, _ = w.Write([]byte(body))
}

// servePath turns an OpenAPI template into a net/http ServeMux pattern. The two
// use the same {name} syntax, so this is identity today; it exists so a template
// that stops being one is a compile-time concern here rather than a silent
// mis-route.
func servePath(template string) string { return template }
