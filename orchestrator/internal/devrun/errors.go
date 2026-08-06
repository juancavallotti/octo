package devrun

import "errors"

var (
	// ErrUnavailable is returned when dev runs are not configured — no Kubernetes
	// access, or a missing image or hash secret. Dev runs are all-or-nothing: an
	// orchestrator that cannot reach a cluster has no dev-run feature rather than a
	// degraded one.
	ErrUnavailable = errors.New("dev runs unavailable")
	// ErrNotFound is returned when no dev run exists for the given id — including
	// when one exists but belongs to another user, so a probe cannot distinguish
	// "not yours" from "not running".
	ErrNotFound = errors.New("dev run not found")
	// ErrIntegrationNotFound is returned when the integration to run does not exist.
	ErrIntegrationNotFound = errors.New("integration not found")
	// ErrUserRequired is returned when a user-facing operation was asked for without
	// saying whose it is. Refused rather than defaulted, because the default an empty
	// user id would take is "every user's dev runs" — and the operations that would
	// then widen are Stop and Reload.
	ErrUserRequired = errors.New("a dev run is always addressed on behalf of a user")
	// ErrNotStarted is returned when a dev run exists but has no pod to read from yet:
	// it was created moments ago, or the scheduler has not placed it. Distinct from
	// ErrNotFound because the caller should wait rather than start it again.
	ErrNotStarted = errors.New("dev run has no pod yet")
	// ErrUnauthorized is returned when a sidecar presents a token that does not
	// match its dev run's recorded hash.
	ErrUnauthorized = errors.New("dev run token rejected")
	// ErrHostTaken is returned when the derived public host is already published by a
	// different user's live dev run. At 36^8 this is unreachable in practice, but a
	// silent collision would hand one user another user's public traffic, so it is
	// refused rather than resolved — resolving it (advancing to a second label) would
	// need storage to stay stable, which is the thing this design does without.
	ErrHostTaken = errors.New("derived dev-run host is already in use")
	// ErrSidecarUnreachable is returned when the dev run's sidecar could not be
	// commanded. The workload exists; it is not answering yet, or not any more.
	ErrSidecarUnreachable = errors.New("dev run sidecar unreachable")
)
