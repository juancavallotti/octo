// Package bus publishes orchestrator events to NATS for cross-node fan-out: the
// platform BFF subscribes and relays them to browsers as SSE. It is intentionally
// tiny — a Publisher interface with a NATS-backed implementation and a noop used
// when NATS_URL is unset — so the orchestrator still runs standalone (local
// `go run`, or a single-node deploy without a broker) with the feature inert.
package bus

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go"
)

// Publisher fans a message out to subscribers on a subject. Publishing is
// fire-and-forget: an error is logged, not returned, so a broker hiccup never
// blocks the caller (the deployment informer callback) or fails a write.
type Publisher interface {
	Publish(subject string, data []byte)
	// Reachable reports whether the broker is currently connected, or why not.
	//
	// It exists for the admin health page rather than for the publish path, which
	// deliberately never asks: publishing is fire-and-forget precisely so a broker
	// hiccup cannot block a write, and a check before each Publish would reintroduce
	// exactly the coupling that design avoids.
	Reachable() error
	Close()
}

type natsPublisher struct{ conn *nats.Conn }

func (p natsPublisher) Publish(subject string, data []byte) {
	if err := p.conn.Publish(subject, data); err != nil {
		slog.Error("bus publish", "subject", subject, "error", err)
	}
}

// Reachable reads the connection's own state rather than sending anything. The
// client reconnects on its own, so "connected" is a fact it already tracks, and a
// probe that published a message to find out would put a message on a subject for
// the sake of asking.
func (p natsPublisher) Reachable() error {
	if p.conn.IsConnected() {
		return nil
	}
	if err := p.conn.LastError(); err != nil {
		return err
	}
	return errNotConnected
}

func (p natsPublisher) Close() { p.conn.Close() }

// errNotConnected is what a disconnected client reports when it has no error of
// its own to offer — during a reconnect, for instance, where nothing has failed
// yet but nothing is connected either.
var errNotConnected = errors.New("bus: not connected to nats")

type noopPublisher struct{}

func (noopPublisher) Publish(string, []byte) {}

// Reachable on the noop reports the truth: there is no broker to reach. Callers
// that care about the distinction between "unconfigured" and "down" check the URL
// instead — a noop only exists when there was none.
func (noopPublisher) Reachable() error { return errNoBroker }

func (noopPublisher) Close() {}

var errNoBroker = errors.New("bus: no broker configured")

// NewPublisher connects to NATS at natsURL and returns a Publisher. An empty
// natsURL yields a noop Publisher (standalone mode, no broker) rather than an
// error, so callers can wire it unconditionally.
func NewPublisher(natsURL string) (Publisher, error) {
	if natsURL == "" {
		return noopPublisher{}, nil
	}
	conn, err := nats.Connect(natsURL, nats.Name("octo-orchestrator"))
	if err != nil {
		return nil, fmt.Errorf("bus: connect nats %q: %w", natsURL, err)
	}
	return natsPublisher{conn: conn}, nil
}
