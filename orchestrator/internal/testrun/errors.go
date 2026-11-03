package testrun

import "errors"

var (
	// ErrNotFound is returned when a test run does not exist for that deployment.
	ErrNotFound = errors.New("test run not found")
	// ErrUnavailable is returned when this install cannot run tests: there is no
	// cluster, or no standalone runtime image to run them in.
	ErrUnavailable = errors.New("test runs unavailable")
	// ErrNoSuites is returned when the snapshot under test has no test suites.
	ErrNoSuites = errors.New("the integration has no test suites")
	// ErrRuntimeMismatch is returned when the image test runs execute in is a
	// different release from the one a rollout would install, so a pass would
	// say nothing about the runtime it authorizes.
	ErrRuntimeMismatch = errors.New("the test image and the deployed runtime are different releases")
)
