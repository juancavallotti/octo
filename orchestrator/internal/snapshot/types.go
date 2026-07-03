// Package snapshot is the orchestrator feature module for integration version
// tags: a snapshot freezes an integration's definition — and a copy of its
// resources — under a named tag so a deploy can ship that frozen definition and
// resource set rather than the live ones. Tags are immutable and unique per
// integration. The module follows the same repository/service/handler shape as
// the folder and integration modules.
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

// Resource is one of an integration's resources frozen under a snapshot when the
// integration was tagged. It belongs to a snapshot and never changes, so it
// carries neither an integration id nor a last_updated stamp. Kind and Name
// mirror the live resource (kind is "env" or "template"; name is the path-like id
// the config references).
type Resource struct {
	ID         string
	SnapshotID string
	Kind       string
	Name       string
	Content    string
	CreatedAt  time.Time
}
