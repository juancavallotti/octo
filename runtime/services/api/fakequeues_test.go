package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// queueBackend is the fake's queue plane: per-subject FIFOs with ack deadlines
// and redelivery, which is what lets a test assert that a nacked message comes
// back rather than that a POST was made.
type queueBackend struct {
	mu       sync.Mutex
	pending  map[string][]*fakeDelivery
	inflight map[string]*fakeDelivery
	next     int
	// replies holds what a subscriber answered, keyed by replyTo.
	replies map[string]messageWire
	waiters map[string]chan messageWire
	// pollDelay makes receive hold the request open, so a test can exercise the
	// empty-poll path without waiting a real poll timeout.
	pollDelay time.Duration
	// handedOut counts every delivery ever made, cumulatively. It is deliberately
	// not derived from the pending and in-flight maps: an acknowledged delivery
	// leaves both, so a test asserting on a redelivery would race the ack and read
	// zero.
	handedOut int
}

type fakeDelivery struct {
	id      string
	subject string
	replyTo string
	msg     messageWire
	// attempts counts how many times this message has been handed out, so a test
	// can tell a redelivery from a first delivery.
	attempts int
}

func newQueueBackend() *queueBackend {
	return &queueBackend{
		pending:  map[string][]*fakeDelivery{},
		inflight: map[string]*fakeDelivery{},
		replies:  map[string]messageWire{},
		waiters:  map[string]chan messageWire{},
	}
}

func (b *queueBackend) install(f *fake) {
	f.mux.HandleFunc("POST /v1/queues/{subject}/publish", b.publish)
	f.mux.HandleFunc("POST /v1/queues/{subject}/request", b.request)
	f.mux.HandleFunc("POST /v1/queues/{subject}/receive", b.receive)
	f.mux.HandleFunc("POST /v1/queues/{subject}/ack", b.ack)
	f.mux.HandleFunc("POST /v1/queues/{subject}/nack", b.nack)
	f.mux.HandleFunc("POST /v1/queues/reply", b.reply)
}

func (b *queueBackend) publish(w http.ResponseWriter, r *http.Request) {
	var in publishRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	b.enqueue(r.PathValue("subject"), in.Message, "")
	w.WriteHeader(http.StatusAccepted)
}

// request enqueues the message with a reply address and waits for the answer.
func (b *queueBackend) request(w http.ResponseWriter, r *http.Request) {
	var in requestRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	b.next++
	replyTo := fmt.Sprintf("reply-%d", b.next)
	answer := make(chan messageWire, 1)
	b.waiters[replyTo] = answer
	b.mu.Unlock()

	b.enqueue(r.PathValue("subject"), in.Message, replyTo)

	select {
	case msg := <-answer:
		writeJSON(w, requestResponse{Message: msg})
	case <-time.After(time.Duration(in.TimeoutSeconds) * time.Second):
		http.Error(w, "no reply", http.StatusGatewayTimeout)
	}
}

func (b *queueBackend) enqueue(subject string, msg messageWire, replyTo string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	b.pending[subject] = append(b.pending[subject], &fakeDelivery{
		id: fmt.Sprintf("d-%d", b.next), subject: subject, replyTo: replyTo, msg: msg,
	})
}

// receive hands out up to maxMessages, answering 204 when there is nothing —
// which is the contract's signal that the long poll expired, not an error.
func (b *queueBackend) receive(w http.ResponseWriter, r *http.Request) {
	var in receiveRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	subject := r.PathValue("subject")

	b.mu.Lock()
	delay := b.pollDelay
	queue := b.pending[subject]
	if len(queue) == 0 {
		b.mu.Unlock()
		if delay > 0 {
			time.Sleep(delay)
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	n := min(in.MaxMessages, len(queue))
	batch := queue[:n]
	b.pending[subject] = queue[n:]
	out := receiveResponse{}
	for _, d := range batch {
		d.attempts++
		b.handedOut++
		b.inflight[d.id] = d
		out.Messages = append(out.Messages, delivery{
			DeliveryID: d.id, ReplyTo: d.replyTo, Message: d.msg,
		})
	}
	b.mu.Unlock()
	writeJSON(w, out)
}

func (b *queueBackend) ack(w http.ResponseWriter, r *http.Request) {
	var in settleRequest
	_ = json.NewDecoder(r.Body).Decode(&in)
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, id := range in.DeliveryIDs {
		delete(b.inflight, id)
	}
	w.WriteHeader(http.StatusNoContent)
}

// nack returns the delivery to the front of its queue after the requested delay,
// so it is redelivered.
//
// The delay is honoured rather than ignored because it is the thing standing
// between a permanently failing handler and a hot loop, and a fake that dropped
// it would let that regress unnoticed. nackScale shortens it for the tests.
func (b *queueBackend) nack(w http.ResponseWriter, r *http.Request) {
	var in settleRequest
	_ = json.NewDecoder(r.Body).Decode(&in)
	delay := time.Duration(in.DelaySeconds) * time.Second / nackScale

	b.mu.Lock()
	requeue := make([]*fakeDelivery, 0, len(in.DeliveryIDs))
	for _, id := range in.DeliveryIDs {
		if d, ok := b.inflight[id]; ok {
			delete(b.inflight, id)
			requeue = append(requeue, d)
		}
	}
	b.mu.Unlock()

	time.AfterFunc(delay, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		for _, d := range requeue {
			b.pending[d.subject] = append([]*fakeDelivery{d}, b.pending[d.subject]...)
		}
	})
	w.WriteHeader(http.StatusNoContent)
}

// nackScale compresses the nack delay so a redelivery test does not wait the full
// production interval. The delay is still honoured, just faster.
const nackScale = 25

func (b *queueBackend) reply(w http.ResponseWriter, r *http.Request) {
	var in replyRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	b.replies[in.ReplyTo] = in.Message
	waiter := b.waiters[in.ReplyTo]
	b.mu.Unlock()
	if waiter != nil {
		waiter <- in.Message
	}
	w.WriteHeader(http.StatusNoContent)
}

// attempts reports how many deliveries the backend has made, cumulatively.
func (b *queueBackend) attempts(string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.handedOut
}
