package bundle

import "errors"

var (
	// ErrGone reports that this dev run no longer addresses anything real: the
	// orchestrator answered 404. Its integration was deleted, or the run was torn
	// down and this pod outlived that decision.
	//
	// This is the one TERMINAL pull failure. A 5xx or a timeout is retried against
	// whatever is already in the workspace; a 404 means no future retry can succeed,
	// so the run is expired and the process exits rather than serving a definition
	// nothing can account for.
	ErrGone = errors.New("dev run no longer exists")

	// ErrUnauthorized reports that the orchestrator rejected this sidecar's dev-run
	// token. NOT terminal: the likely cause is a misconfigured pod, and one that
	// deleted itself over a credential problem would destroy the evidence. It
	// retries, loudly.
	ErrUnauthorized = errors.New("dev-run token rejected")
)
