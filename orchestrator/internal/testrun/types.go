// Package testrun runs a deployment's test suites against the runtime release
// the install deploys, as a Job of its own, and can roll the deployment onto the
// tested snapshot when they pass.
//
// A run is recorded before its Job exists and finished exactly once: whichever
// of the watcher that started it or the sweep reaches the end first records the
// verdict, and only that one may roll out.
package testrun

import (
	"encoding/json"
	"time"
)

// Status is where a test run is.
type Status string

const (
	// StatusRunning is a run whose Job has not finished.
	StatusRunning Status = "running"
	// StatusPassed is a run in which every case passed.
	StatusPassed Status = "passed"
	// StatusFailed is a run in which a case ran and did not do what it said.
	StatusFailed Status = "failed"
	// StatusErrored is a run that could not test anything: a suite or the config
	// is wrong, or the Job never ran.
	StatusErrored Status = "errored"
	// StatusTimedOut is a run killed for taking too long.
	StatusTimedOut Status = "timed_out"
)

// Run is one run of a deployment's suites.
type Run struct {
	ID           string
	DeploymentID string
	SnapshotID   string
	// RuntimeVersion is the runtime release the suites ran against.
	RuntimeVersion string
	Status         Status
	// RolloutOnPass asks for the deployment to be rolled onto SnapshotID when the
	// run passes; RolledOutAt says it was.
	RolloutOnPass bool
	RolledOutAt   *time.Time
	// Report is dolphin's JSON report, nil when the run produced none.
	Report json.RawMessage
	// Error says why a run errored, or why a rollout it asked for did not happen.
	Error      string
	CreatedBy  string
	CreatedAt  time.Time
	FinishedAt *time.Time
}

// Result is the verdict a finished run records.
type Result struct {
	Status Status
	Report json.RawMessage
	Error  string
}
