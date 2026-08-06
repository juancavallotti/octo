package bundle

import "errors"

var (
	// ErrGone reports that this dev run no longer addresses anything real: the
	// orchestrator answered 404. Its integration was deleted, or the run was torn
	// down and this pod outlived that decision.
	//
	// This is the one TERMINAL pull failure, and the distinction is the whole point
	// of having a named error. A 5xx or a timeout means the orchestrator is having a
	// bad minute, and the right response is to back off and keep serving whatever is
	// already in the workspace. A 404 means no future retry can succeed, and a
	// sidecar that treats it as transient sits there serving a definition nobody can
	// account for — so instead it asks the orchestrator to expire the run and exits.
	ErrGone = errors.New("dev run no longer exists")

	// ErrUnauthorized reports that the orchestrator rejected this sidecar's dev-run
	// token. Deliberately NOT terminal: the honest cause is a misconfigured pod, and
	// a pod that deletes itself over a credential problem destroys the evidence. It
	// retries, loudly, so the loop is visible to whoever has to fix it.
	ErrUnauthorized = errors.New("dev-run token rejected")
)
