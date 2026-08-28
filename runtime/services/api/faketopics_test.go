package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// topicBackend is the fake's broadcast plane: a per-subject log and a cursor per
// subscription, which is what makes fan-out expressible over a pull API.
type topicBackend struct {
	mu sync.Mutex
	// log holds every message published to a subject, in order.
	log map[string][]messageWire
	// cursor is how far each subscription has read.
	cursor map[string]int
	// subject records which subject a subscription belongs to.
	subject map[string]string
	// systemPublishes records subjects published with the system flag set.
	systemPublishes []string
	next            int
	pollDelay       time.Duration
}

func newTopicBackend() *topicBackend {
	return &topicBackend{
		log:     map[string][]messageWire{},
		cursor:  map[string]int{},
		subject: map[string]string{},
	}
}

func (b *topicBackend) install(f *fake) {
	f.mux.HandleFunc("POST /v1/topics/{subject}/publish", b.publish)
	f.mux.HandleFunc("POST "+pathTopicSubs, b.subscribe)
	f.mux.HandleFunc("POST /v1/topics/{subject}/receive", b.receive)
	f.mux.HandleFunc("POST /v1/topics/{subject}/ack", b.ack)
	f.mux.HandleFunc("DELETE "+pathTopicSubs+"/{subscriptionId}", b.unsubscribe)
}

func (b *topicBackend) publish(w http.ResponseWriter, r *http.Request) {
	var in topicPublishRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	subject := r.PathValue("subject")

	b.mu.Lock()
	b.log[subject] = append(b.log[subject], in.Message)
	if in.System {
		b.systemPublishes = append(b.systemPublishes, subject)
	}
	b.mu.Unlock()
	w.WriteHeader(http.StatusAccepted)
}

// subscribe mints a cursor starting at the end of the log, so a subscriber sees
// what is published after it joins rather than the whole history.
func (b *topicBackend) subscribe(w http.ResponseWriter, r *http.Request) {
	subject := r.PathValue("subject")

	b.mu.Lock()
	b.next++
	id := fmt.Sprintf("sub-%d", b.next)
	b.cursor[id] = len(b.log[subject])
	b.subject[id] = subject
	b.mu.Unlock()
	writeJSON(w, subscribeResponse{SubscriptionID: id})
}

// receive hands each subscription everything past its own cursor, which is what
// makes every subscriber see every message.
func (b *topicBackend) receive(w http.ResponseWriter, r *http.Request) {
	var in receiveRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	subject := r.PathValue("subject")

	b.mu.Lock()
	at, known := b.cursor[in.SubscriptionID]
	entries := b.log[subject]
	delay := b.pollDelay
	if !known {
		b.mu.Unlock()
		http.Error(w, "no such subscription", http.StatusNotFound)
		return
	}
	if at >= len(entries) {
		b.mu.Unlock()
		if delay > 0 {
			time.Sleep(delay)
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	end := min(at+in.MaxMessages, len(entries))
	out := receiveResponse{}
	for i := at; i < end; i++ {
		out.Messages = append(out.Messages, delivery{
			DeliveryID: fmt.Sprintf("%s:%d", in.SubscriptionID, i), Message: entries[i],
		})
	}
	// The cursor moves on delivery and the ack confirms it; a redelivery model
	// would need in-flight tracking this fake does not need to exercise.
	b.cursor[in.SubscriptionID] = end
	b.mu.Unlock()
	writeJSON(w, out)
}

func (b *topicBackend) ack(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (b *topicBackend) unsubscribe(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("subscriptionId")
	b.mu.Lock()
	delete(b.cursor, id)
	delete(b.subject, id)
	b.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// subscriptions reports how many cursors are live, so a test can see one left
// behind by a Close that did not unregister.
func (b *topicBackend) subscriptions() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.cursor)
}
