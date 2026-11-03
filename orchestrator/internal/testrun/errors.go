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
)
