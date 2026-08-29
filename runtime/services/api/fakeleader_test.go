package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// leaderBackend is the fake's leader table: one holder per key, with an expiry.
// The campaign endpoint grants when the key is free, when it has expired, or when
// the caller already holds it — which is how one endpoint serves both the first
// claim and every renewal.
type leaderBackend struct {
	mu      sync.Mutex
	holders map[string]*fakeLeader
	next    int
	// offline, when set, refuses every campaign call, standing in for a platform
	// the runtime cannot reach.
	offline bool
}

type fakeLeader struct {
	id      string
	holder  string
	expires time.Time
}

func newLeaderBackend() *leaderBackend {
	return &leaderBackend{holders: map[string]*fakeLeader{}}
}

func (b *leaderBackend) install(f *fake) {
	f.mux.HandleFunc("POST /v1/leader/{key}/campaign", b.campaign)
	f.mux.HandleFunc("POST /v1/leader/{key}/resign", b.resign)
}

func (b *leaderBackend) campaign(w http.ResponseWriter, r *http.Request) {
	var in campaignRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	key := r.PathValue("key")

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.offline {
		http.Error(w, "unreachable", http.StatusServiceUnavailable)
		return
	}
	held, exists := b.holders[key]
	free := !exists || time.Now().After(held.expires) || held.holder == in.Holder
	if !free {
		writeJSON(w, campaignResponse{Leader: false, CurrentLeader: held.holder})
		return
	}
	if !exists || held.holder != in.Holder {
		b.next++
		held = &fakeLeader{id: fmt.Sprintf("leader-%d", b.next), holder: in.Holder}
		b.holders[key] = held
	}
	held.expires = time.Now().Add(time.Duration(in.TTLSeconds) * time.Second)
	writeJSON(w, campaignResponse{Leader: true, LeaseID: held.id})
}

func (b *leaderBackend) resign(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.holders, r.PathValue("key"))
	w.WriteHeader(http.StatusNoContent)
}

// take makes somebody else the holder of key, so a test can watch a campaign lose.
func (b *leaderBackend) take(key, holder string, ttl time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	b.holders[key] = &fakeLeader{
		id: fmt.Sprintf("leader-%d", b.next), holder: holder, expires: time.Now().Add(ttl),
	}
}

// setOffline makes every campaign call fail.
func (b *leaderBackend) setOffline(v bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.offline = v
}
