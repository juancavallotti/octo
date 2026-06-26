// Package snapshot is the orchestrator feature module for integration version
// tags: a snapshot freezes an integration's definition under a named tag so a
// deploy can ship that frozen definition rather than the live one. Tags are
// immutable and unique per integration. The module follows the same
// repository/service/handler shape as the folder and integration modules.
package snapshot

import "time"

// Snapshot is a frozen integration definition captured under a tag. IDs are UUIDs
// in canonical text form.
type Snapshot struct {
	ID            string
	IntegrationID string
	Tag           string
	Definition    string
	CreatedAt     time.Time
}
