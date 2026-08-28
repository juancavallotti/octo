package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// leaseBackend is the fake's claim table: exclusive names with expiry, on a clock
// the test controls so a claim can be aged without waiting for one.
type leaseBackend struct {
	mu     sync.Mutex
	claims map[string]*fakeClaim
	byID   map[string]*fakeClaim
	next   int
	now    time.Time
	// renewFails, when set, refuses every renewal — the way a claim gets lost
	// without a test having to stop the server.
	renewFails bool
}

type fakeClaim struct {
	id      string
	name    string
	holder  string
	expires time.Time
}

func newLeaseBackend() *leaseBackend {
	return &leaseBackend{
		claims: map[string]*fakeClaim{},
		byID:   map[string]*fakeClaim{},
		now:    time.Now(),
	}
}

func (b *leaseBackend) install(f *fake) {
	f.mux.HandleFunc("POST /v1/leases/acquire", b.acquire)
	f.mux.HandleFunc("POST /v1/leases/{leaseId}/renew", b.renew)
	f.mux.HandleFunc("POST /v1/leases/{leaseId}/release", b.release)
}

// advance moves the fake's clock, expiring claims.
func (b *leaseBackend) advance(d time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.now = b.now.Add(d)
}

func (b *leaseBackend) acquire(w http.ResponseWriter, r *http.Request) {
	var in acquireRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if held, ok := b.claims[in.Name]; ok && held.expires.After(b.now) {
		writeJSON(w, acquireResponse{Acquired: false, Holder: held.holder})
		return
	}
	b.next++
	claim := &fakeClaim{
		id:      fmt.Sprintf("lease-%d", b.next),
		name:    in.Name,
		holder:  in.Holder,
		expires: b.now.Add(time.Duration(in.TTLSeconds) * time.Second),
	}
	b.claims[in.Name] = claim
	b.byID[claim.id] = claim
	writeJSON(w, acquireResponse{Acquired: true, LeaseID: claim.id})
}

func (b *leaseBackend) renew(w http.ResponseWriter, r *http.Request) {
	var in renewRequest
	_ = json.NewDecoder(r.Body).Decode(&in)

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.renewFails {
		http.Error(w, "the claim is no longer yours", http.StatusConflict)
		return
	}
	claim, ok := b.byID[r.PathValue("leaseId")]
	if !ok {
		http.Error(w, "no such claim", http.StatusNotFound)
		return
	}
	claim.expires = b.now.Add(time.Duration(in.TTLSeconds) * time.Second)
	w.WriteHeader(http.StatusNoContent)
}

func (b *leaseBackend) release(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if claim, ok := b.byID[r.PathValue("leaseId")]; ok {
		delete(b.byID, claim.id)
		if b.claims[claim.name] == claim {
			delete(b.claims, claim.name)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// holderOf reports who holds a name, for assertions.
func (b *leaseBackend) holderOf(name string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if claim, ok := b.claims[name]; ok {
		return claim.holder
	}
	return ""
}
