// Package agentmemory stores what an ai-agent remembers: the live context it
// resumes from, the durable record of the conversations it has had, and the
// curated facts it chose to keep about the people it talks to.
//
// It is keyed by the INTEGRATION, not by the deployment, and that is the reason
// it exists apart from the kv store. An integration's stored working state
// belongs to the deployment that wrote it and is purged with it, which is right
// for a cache and wrong for a conversation somebody had: undeploy-then-deploy is
// an ordinary recovery move, and it silently destroyed every conversation on the
// installation (#362). The runtime never sends an integration id — a pod knows
// its deployment and nothing else — so the handler resolves the one from the
// other, the same immutable relation traces resolves at ingest.
package agentmemory

import "time"

// Thread is one conversation: what a listing shows without opening it.
type Thread struct {
	ID             string    `json:"id"`
	AgentID        string    `json:"agentId"`
	ThreadKey      string    `json:"threadKey"`
	UserID         string    `json:"userId,omitempty"`
	Title          string    `json:"title,omitempty"`
	Version        int64     `json:"version"`
	TurnCount      int       `json:"turnCount"`
	CreatedAt      time.Time `json:"createdAt"`
	LastActivityAt time.Time `json:"lastActivityAt"`
}

// Working is an agent's live context at a point in a run.
//
// Payload is opaque: it is the runtime's serialized transcript, and nothing here
// ever looks inside it. That is why the column is bytea rather than jsonb, and
// why the transcript's shape can change without this package knowing.
type Working struct {
	Version   int64     `json:"version"`
	Iteration int       `json:"iteration"`
	Tokens    int       `json:"tokens"`
	Payload   []byte    `json:"-"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Turn is one entry in the durable record. Attrs is opaque JSON the runtime
// writes and reads back — how many iterations a turn took, whether the question
// was ever answered.
type Turn struct {
	Seq       int64          `json:"seq"`
	Role      string         `json:"role"`
	Text      string         `json:"text"`
	Tokens    int            `json:"tokens,omitempty"`
	Attrs     map[string]any `json:"attrs,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

// UserMemory is one curated fact an agent kept about a person.
type UserMemory struct {
	Name      string    `json:"name"`
	Value     string    `json:"value"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Hit is one search result. Kind is HitTurn or HitUser.
type Hit struct {
	Kind      string  `json:"kind"`
	ThreadKey string  `json:"threadKey,omitempty"`
	Name      string  `json:"name,omitempty"`
	Text      string  `json:"text"`
	Seq       int64   `json:"seq,omitempty"`
	Score     float64 `json:"score"`
}

// The two kinds a Hit can be.
const (
	HitTurn = "turn"
	HitUser = "user"
)

// Scopes a search can be narrowed to. An empty scope searches both.
const (
	ScopeAll   = ""
	ScopeTurns = "turns"
	ScopeUser  = "user"
)

// Query asks for the memory most relevant to Text.
type Query struct {
	AgentID   string `json:"agentId"`
	UserID    string `json:"userId,omitempty"`
	ThreadKey string `json:"threadKey,omitempty"`
	Text      string `json:"text"`
	Scope     string `json:"scope,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

// Ref addresses one agent's memory within an integration.
type Ref struct {
	IntegrationID string
	AgentID       string
	ThreadKey     string
	UserID        string
}

// Page is a keyset cursor into a listing.
type Page struct {
	Cursor string
	Limit  int
}

// ThreadPage is a listing plus the cursor that continues it.
type ThreadPage struct {
	Threads []Thread `json:"threads"`
	Next    string   `json:"next,omitempty"`
}

// Transcript is a conversation and a page of its turns.
type Transcript struct {
	Thread Thread `json:"thread"`
	Turns  []Turn `json:"turns"`
	Next   string `json:"next,omitempty"`
}

// AgentSummary is one row of the agent listing an operator picks from.
type AgentSummary struct {
	AgentID        string    `json:"agentId"`
	ThreadCount    int       `json:"threadCount"`
	LastActivityAt time.Time `json:"lastActivityAt"`
}
