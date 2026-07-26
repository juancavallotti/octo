package services

import "sync/atomic"

// State is where the runtime is in its lifecycle, as a hosted service sees it. It
// is the readiness signal: exactly one state means ready, and the rest are the
// reasons a probe should say no.
type State string

const (
	// StateStarting is the state a process begins in: the CLI has parsed its flags
	// and the hosted services are up, but connectors and flows are not. A readiness
	// probe answering during this window is the point of having one — the process is
	// alive and refusing traffic on purpose.
	StateStarting State = "starting"
	// StateReady means every connector and flow of the current generation started.
	// Sources are bound, so the runtime is accepting traffic.
	StateReady State = "ready"
	// StateReloading means a watched config changed and the previous generation is
	// being torn down and rebuilt. Readiness goes false for the gap.
	StateReloading State = "reloading"
	// StateDraining means shutdown has begun. It is set as soon as the run context
	// is cancelled, BEFORE flows drain, so a load balancer stops sending traffic
	// while there are still workers to finish what is in flight. This is most of the
	// value of separating readiness from liveness.
	StateDraining State = "draining"
	// StateStopped means the runtime has stopped and nothing is being served.
	StateStopped State = "stopped"
)

// Health is the runtime's readiness gate: the CLI writes it as the lifecycle
// advances, and a hosted service reads it to answer probes. It is safe for
// concurrent use.
//
// There is no liveness gate, deliberately. Liveness asks whether the process is
// wedged, and a dedicated server answering at all is the answer — a gate could
// only ever report "yes" or fail to be read.
type Health struct {
	state atomic.Pointer[State]
}

// NewHealth returns a gate in StateStarting.
func NewHealth() *Health {
	h := &Health{}
	h.SetState(StateStarting)
	return h
}

// SetState records where the runtime now is.
func (h *Health) SetState(state State) {
	h.state.Store(&state)
}

// State returns the current lifecycle state.
func (h *Health) State() State {
	if state := h.state.Load(); state != nil {
		return *state
	}
	return StateStarting
}

// Ready reports whether the runtime is serving: every connector and flow started,
// and shutdown has not begun.
func (h *Health) Ready() bool { return h.State() == StateReady }
