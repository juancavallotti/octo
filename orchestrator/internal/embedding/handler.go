package embedding

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
)

// requestTimeoutHTTP bounds the work behind one status request: a health probe
// against the embedding server plus one counting query.
const requestTimeoutHTTP = 10 * time.Second

// Counter reports how much of the store is still waiting to be vectorized.
// *agentmemory.Repo satisfies it; it is an interface so this package does not
// depend on that one.
type Counter interface {
	PendingCount(ctx context.Context) (int, error)
}

// Handler serves the embedding status.
//
// READ ONLY, and that is the whole design rather than an omission. Embedding
// configuration is deploy-time — the provider, the model and the key are chart
// values on the embedding server — because the model cannot be changed once
// anything has been embedded and a control that must never be touched does not
// belong behind a Save button. What an operator wants from a page is therefore
// not a form but an answer: is it on, what is it using, and how much of the
// store has it got through.
//
// No credential passes through here in either direction. There is none to
// expose: this orchestrator holds a URL, and the server it asks reports what it
// is configured to do without reporting what it authenticates with.
type Handler struct {
	client  *Client
	counter Counter
}

// NewHandler returns the status handler. A nil counter reports nothing pending,
// which is what an orchestrator with no database has to say.
func NewHandler(client *Client, counter Counter) *Handler {
	return &Handler{client: client, counter: counter}
}

// Register mounts the route.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /settings/embedding", h.get)
}

// statusResponse is what the admin page renders.
type statusResponse struct {
	Status
	// Pending is what is left of the backfill. Configuring a provider does not
	// make search semantic; it makes it become semantic, and this is how much is
	// still waiting. Zero is the ordinary answer and says nothing is outstanding.
	//
	// Deliberately not paired with an "embedded" total: counting rows that HAVE a
	// vector cannot use an index, so it read both tables end to end on every load
	// to draw a progress bar.
	Pending int `json:"pending"`
}

// get reports the embedding server's configuration and the backfill's progress.
//
// @Summary     Embedding status
// @Description Whether this installation has an embedding server, what it is configured to use, and how much of agent memory is still waiting to be vectorized. Read only: the provider, model and key are deploy-time chart values, because changing the model discards every stored vector.
// @Tags        settings
// @Produce     json
// @Success     200 {object} statusResponse
// @Router      /settings/embedding [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeoutHTTP)
	defer cancel()

	out := statusResponse{Status: h.client.Check(ctx)}
	if h.counter != nil {
		pending, err := h.counter.PendingCount(ctx)
		if err != nil {
			// The count is the least of what this says, and the half that matters —
			// whether the server is there and answering — is already in hand.
			// Reported, and served without it.
			slog.Warn("embedding status: pending count unavailable", "error", err)
		} else {
			out.Pending = pending
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
