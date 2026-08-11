package agent

import "errors"

var (
	// ErrClusterUnavailable — the orchestrator has no in-cluster access, so nothing
	// can be deployed. Reading the status still works and says so.
	ErrClusterUnavailable = errors.New("agent: kubernetes is not available")

	// ErrNotInstalled — the operation needs an install that has not happened.
	ErrNotInstalled = errors.New("agent: not installed")

	// ErrNotDeployed — the agent is installed but nothing is running, so there is no
	// deployment to roll out or trace.
	ErrNotDeployed = errors.New("agent: not deployed")

	// ErrUnknownProvider — the site's LLM settings name a provider this orchestrator
	// cannot map to a runtime connector. Its own error because the fix is in the LLM
	// settings, not here.
	ErrUnknownProvider = errors.New("agent: unknown llm provider")
)
