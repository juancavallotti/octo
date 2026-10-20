package client

import (
	"encoding/json"
	"time"
)

// These are the shapes api/static/openapi.yaml describes, named as it names
// them. Fields the CLI never shows are kept anyway, so `-o json` gives back
// everything the API said.

// User is the account a credential acts as.
type User struct {
	ID          string     `json:"id"`
	Subject     string     `json:"subject,omitempty"`
	Email       string     `json:"email"`
	Name        string     `json:"name"`
	Roles       []string   `json:"roles"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastLoginAt *time.Time `json:"lastLoginAt"`
}

// Integration is one integration, with its YAML.
type Integration struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Icon           string    `json:"icon,omitempty"`
	Definition     string    `json:"definition,omitempty"`
	LastUpdated    time.Time `json:"lastUpdated"`
	CreatedBy      string    `json:"createdBy,omitempty"`
	CreatedByEmail string    `json:"createdByEmail,omitempty"`
	CreatedByName  string    `json:"createdByName,omitempty"`
	UpdatedBy      string    `json:"updatedBy,omitempty"`
	UpdatedByEmail string    `json:"updatedByEmail,omitempty"`
	UpdatedByName  string    `json:"updatedByName,omitempty"`
}

// File is one of an integration's config files. Listing leaves Content empty.
type File struct {
	Path        string    `json:"path"`
	Content     string    `json:"content,omitempty"`
	LastUpdated time.Time `json:"lastUpdated,omitzero"`
}

// ResourceInput is what a caller sends to create or update a resource.
type ResourceInput struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

// Resource is a resource as stored.
type Resource struct {
	ResourceInput
	ID            string    `json:"id"`
	IntegrationID string    `json:"integrationId"`
	CreatedAt     time.Time `json:"createdAt"`
	LastUpdated   time.Time `json:"lastUpdated"`
}

// Version is an integration frozen under a tag.
type Version struct {
	ID            string    `json:"id"`
	IntegrationID string    `json:"integrationId"`
	Tag           string    `json:"tag"`
	Definition    string    `json:"definition,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

// EnvBinding binds one declared env var to a literal value or a cluster secret.
type EnvBinding struct {
	Value  string `json:"value,omitempty"`
	Secret string `json:"secret,omitempty"`
}

// DeploymentSettings is what a deploy asks for. Only SnapshotID is required.
type DeploymentSettings struct {
	SnapshotID string                `json:"snapshotId"`
	Replicas   int                   `json:"replicas,omitempty"`
	Slug       string                `json:"slug,omitempty"`
	Expose     string                `json:"expose,omitempty"`
	Subdomain  string                `json:"subdomain,omitempty"`
	Env        map[string]EnvBinding `json:"env,omitempty"`
	Tracing    *bool                 `json:"tracing,omitempty"`
	Access     []string              `json:"access,omitempty"`
	Runner     string                `json:"runner,omitempty"`
}

// Rollout is what a rolling update changes. A nil field keeps what the
// deployment has.
type Rollout struct {
	SnapshotID string                `json:"snapshotId,omitempty"`
	Env        map[string]EnvBinding `json:"env,omitempty"`
	Tracing    *bool                 `json:"tracing,omitempty"`
}

// Pod is one of a deployment's pods.
type Pod struct {
	Name     string `json:"name"`
	Phase    string `json:"phase"`
	Ready    bool   `json:"ready"`
	Restarts int    `json:"restarts"`
}

// Deployment is a deployment with its live status. The API describes it as
// open-ended, so it keeps the document it was decoded from and hands that back
// when it is marshalled: nothing the orchestrator adds is lost on `-o json`.
type Deployment struct {
	ID              string    `json:"id"`
	IntegrationID   string    `json:"integrationId"`
	Name            string    `json:"name"`
	Tag             string    `json:"tag"`
	SnapshotID      string    `json:"snapshotId"`
	Status          string    `json:"status"`
	Replicas        int       `json:"replicas"`
	ReadyReplicas   int       `json:"readyReplicas"`
	DesiredReplicas int       `json:"desiredReplicas"`
	Reason          string    `json:"reason"`
	Pods            []Pod     `json:"pods"`
	InternalURL     string    `json:"internalUrl"`
	ExternalURL     string    `json:"externalUrl"`
	CreatedAt       time.Time `json:"createdAt"`
	LastUpdated     time.Time `json:"lastUpdated"`

	raw json.RawMessage
}

// UnmarshalJSON decodes the known fields and keeps the whole document.
func (d *Deployment) UnmarshalJSON(data []byte) error {

	type plain Deployment
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*d = Deployment(p)
	d.raw = append(json.RawMessage(nil), data...)
	return nil
}

// MarshalJSON gives back the document as the API sent it.
func (d Deployment) MarshalJSON() ([]byte, error) {

	if d.raw != nil {
		return d.raw, nil
	}
	type plain Deployment
	return json.Marshal(plain(d))
}

// LogQuery narrows a deployment's stored logs. Zero values are left out.
type LogQuery struct {
	Level  string
	Text   string
	From   time.Time
	To     time.Time
	Before string
	Limit  int
}

// LogPage is one page of stored log entries, newest first.
type LogPage struct {
	Items      []map[string]any `json:"items"`
	NextBefore string           `json:"next_before,omitempty"`
}

// Event is one server-sent event.
type Event struct {
	Name string
	Data string
}
