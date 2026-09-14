// Agent memory: the durable state an agent accumulates across invocations, and
// the one interface every deployment supplies its own storage for.
//
// Three things live here, and they are deliberately not one thing:
//
//   - Working memory is the agent's live context — the transcript it replays to
//     the model. It is compacted to fit a budget, so it is lossy on purpose, and
//     it is checkpointed during a run so an interrupted agent can be resumed.
//   - Conversation history is the durable turn-level record. It is append-only,
//     never compacted, and it is what a person reads when they open a past
//     conversation. Keeping it apart from working memory is the whole point:
//     making room for the model must not destroy the record.
//   - User memory is curated. An agent writes it deliberately, through a tool,
//     when something about a person is worth keeping past the conversation it
//     was learned in. It is not a transcript dump.
//
// All three are addressed by a MemoryRef: an agent, a conversation thread, and
// (for user memory) a person.
package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

// ErrMemoryDisabled is returned by a store that has nowhere to put anything. It
// exists so a caller that reaches a write it should have gated on Enabled gets a
// named error rather than a silent success.
var ErrMemoryDisabled = errors.New("agent memory: no store configured")

// MemoryRef names what a memory operation is about. It carries no tenancy or
// deployment identity: scoping memory to whatever owns it is the store's job,
// resolved where that relation is known, exactly as it is for a KV key.
//
// AgentID names the logical agent. ThreadKey names one conversation with it.
// UserID names the person on the other side, and is empty for an agent that
// serves no particular one.
type MemoryRef struct {
	AgentID   string
	ThreadKey string
	UserID    string
}

// WorkingMemory is an agent's live context at a point in a run.
//
// Payload is opaque to the store. It is the engine's serialized transcript, and
// keeping the store ignorant of its shape is what lets the transcript format
// change without touching a single storage implementation.
//
// Version is the value read, and the value a write must still match. Every
// accepted write returns a higher one.
type WorkingMemory struct {
	Version   int64
	Iteration int
	Tokens    int
	Payload   []byte
	UpdatedAt time.Time
}

// Turn is one entry in the durable conversation record.
//
// Seq orders turns within a thread and is assigned by the store, so two writers
// on one conversation interleave rather than collide. Attrs is opaque JSON for
// whatever the engine wants to remember about the turn that is not its text —
// the iteration count it took, whether it was ever answered.
type Turn struct {
	Seq    int64
	Role   LLMRole
	Text   string
	Tokens int
	Attrs  []byte
	// CreatedAt is set by the store when a turn is appended, and IGNORED on the way
	// in. The append is the event, so the store is the only thing that knows when it
	// happened, and no clock travels over the wire.
	CreatedAt time.Time
}

// Thread is a conversation's metadata: enough to list conversations for an agent
// without reading any of them.
type Thread struct {
	AgentID        string
	ThreadKey      string
	UserID         string
	Title          string
	Version        int64
	TurnCount      int
	CreatedAt      time.Time
	LastActivityAt time.Time
}

// UserMemory is one curated fact an agent chose to keep about a person. Name is
// the agent's own handle for it, and is what an update or a deletion addresses.
type UserMemory struct {
	Name      string
	Value     string
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// MemoryScope selects what a search looks at.
type MemoryScope string

// The scopes a search can be narrowed to.
const (
	MemoryScopeAll   MemoryScope = ""      // turns and user memories
	MemoryScopeTurns MemoryScope = "turns" // conversation history only
	MemoryScopeUser  MemoryScope = "user"  // curated user memory only
)

// MemoryQuery asks a store for the memory most relevant to Text. Every other
// field narrows the search; an empty ThreadKey searches every thread the agent
// has.
type MemoryQuery struct {
	AgentID   string
	UserID    string
	ThreadKey string
	Text      string
	Scope     MemoryScope
	Limit     int
}

// MemoryHit is one search result. Kind is MemoryHitTurn or MemoryHitUser, saying
// which of the two stores it came out of, since the fields that matter differ.
type MemoryHit struct {
	Kind      string
	ThreadKey string
	Name      string
	Text      string
	Seq       int64
	Score     float64
}

// The two kinds a MemoryHit can be.
const (
	MemoryHitTurn = "turn"
	MemoryHitUser = "user"
)

// MemoryCapabilities says what a store can do beyond the interface's guarantees.
//
// Semantic reports whether Search ranks by embedding similarity rather than by
// text matching. It is read per call rather than fixed at startup, because a store
// can gain or lose embeddings while it is running. Search works either way; only
// how well it ranks changes.
type MemoryCapabilities struct {
	Semantic bool
}

// Page is a cursor into a listing. An empty Cursor starts at the beginning; the
// cursor a listing returns continues it.
type Page struct {
	Cursor string
	Limit  int
}

// AgentMemory is the store for everything an agent remembers.
//
// Writes to a versioned object take the version the caller last read and return
// ErrVersionConflict when it no longer matches, so a concurrent update is never
// silently lost. AppendTurns is the exception, and deliberately: appends to a
// conversation commute, so demanding a version would make two writers fight over
// a log that has no conflict to detect.
//
//nolint:interfacebloat // one method per operation over the three stores
type AgentMemory interface {
	// Enabled reports whether this store can hold anything. A disabled store reads
	// empty and its writes return ErrMemoryDisabled.
	Enabled() bool
	// Capabilities describes the optional halves of the store's behaviour.
	Capabilities() MemoryCapabilities

	// LoadWorking returns the agent's live context for a thread. ok is false for a
	// conversation that has none yet.
	LoadWorking(ctx context.Context, ref MemoryRef) (wm WorkingMemory, ok bool, err error)
	// SaveWorking stores the live context, creating the thread if it is new. The
	// caller passes the version it read in wm.Version; zero creates.
	SaveWorking(ctx context.Context, ref MemoryRef, wm WorkingMemory) (newVersion int64, err error)

	// AppendTurns adds completed turns to the durable record, creating the thread
	// if it is new, and returns the thread's new version. The store assigns Seq.
	AppendTurns(ctx context.Context, ref MemoryRef, turns []Turn) (threadVersion int64, err error)
	// ListThreads returns an agent's conversations, most recently active first,
	// optionally narrowed to one user. next is empty when the listing is complete.
	ListThreads(ctx context.Context, agentID, userID string, page Page) (rows []Thread, next string, err error)
	// ReadThread returns a conversation's metadata and a page of its turns in
	// order. next is empty when the transcript is complete.
	ReadThread(ctx context.Context, ref MemoryRef, page Page) (thread Thread, turns []Turn, next string, err error)
	// DeleteThread removes a conversation entirely: its metadata, its working
	// memory and its turns. A missing thread is not an error.
	DeleteThread(ctx context.Context, ref MemoryRef) error
	// SetTitle names a conversation. It is separate from the write path because
	// naming one is a judgement the runtime does not make.
	SetTitle(ctx context.Context, ref MemoryRef, title string) error

	// Memories returns everything the agent has kept about ref.UserID.
	Memories(ctx context.Context, ref MemoryRef) ([]UserMemory, error)
	// PutMemory creates or updates one curated memory by name. expectedVersion 0
	// creates; a positive value must match.
	PutMemory(ctx context.Context, ref MemoryRef, name, value string, expectedVersion int64) (int64, error)
	// DeleteMemory forgets one by name. A missing memory is not an error.
	DeleteMemory(ctx context.Context, ref MemoryRef, name string) error

	// Search returns the memory most relevant to the query, ranked by embedding
	// similarity where the store has embeddings and by text matching where it does
	// not. See MemoryCapabilities.
	Search(ctx context.Context, q MemoryQuery) ([]MemoryHit, error)
}

// noopAgentMemory is the store for a runtime with nowhere to keep memory. It
// reads empty and its writes fail with ErrMemoryDisabled.
//
// It reports Enabled() == false, and a caller is expected to branch on that rather
// than to write and handle the error: an agent with nowhere to remember anything
// is a supported configuration, not a failure.
type noopAgentMemory struct{}

func (noopAgentMemory) Enabled() bool                    { return false }
func (noopAgentMemory) Capabilities() MemoryCapabilities { return MemoryCapabilities{} }

// The deletes report success where the writes report ErrMemoryDisabled: a write
// that vanishes is a lie, whereas a delete against a store that has never held
// anything has achieved exactly what the caller asked for.
func (noopAgentMemory) DeleteThread(context.Context, MemoryRef) error         { return nil }
func (noopAgentMemory) DeleteMemory(context.Context, MemoryRef, string) error { return nil }

func (noopAgentMemory) LoadWorking(context.Context, MemoryRef) (WorkingMemory, bool, error) {
	return WorkingMemory{}, false, nil
}

func (noopAgentMemory) SaveWorking(context.Context, MemoryRef, WorkingMemory) (int64, error) {
	return 0, ErrMemoryDisabled
}

func (noopAgentMemory) AppendTurns(context.Context, MemoryRef, []Turn) (int64, error) {
	return 0, ErrMemoryDisabled
}

func (noopAgentMemory) ListThreads(context.Context, string, string, Page) ([]Thread, string, error) {
	return nil, "", nil
}

func (noopAgentMemory) ReadThread(context.Context, MemoryRef, Page) (Thread, []Turn, string, error) {
	return Thread{}, nil, "", nil
}

func (noopAgentMemory) SetTitle(context.Context, MemoryRef, string) error {
	return ErrMemoryDisabled
}

func (noopAgentMemory) Memories(context.Context, MemoryRef) ([]UserMemory, error) {
	return nil, nil
}

func (noopAgentMemory) PutMemory(context.Context, MemoryRef, string, string, int64) (int64, error) {
	return 0, ErrMemoryDisabled
}

func (noopAgentMemory) Search(context.Context, MemoryQuery) ([]MemoryHit, error) {
	return nil, nil
}

// noopMemory is the shared instance returned when there is no store.
var noopMemory AgentMemory = noopAgentMemory{}

// NoopAgentMemory returns the store for a runtime that keeps no agent memory, and
// is what the no-op services expose.
//
//nolint:ireturn // returns the AgentMemory interface intentionally
func NoopAgentMemory() AgentMemory { return noopMemory }

// memoryContextKey is the context key the forwarded agent-memory context travels
// under. Unexported and of its own type, so nothing outside this package can
// collide with it or set one.
type memoryContextKey struct{}

// WithMemoryContext carries opaque context alongside an agent-memory call: what a
// flow chose to forward with everything its agent remembers.
//
// It rides on the context rather than on MemoryRef or a widened signature
// because no store interprets it. A store that has no use for it — every store
// in-process — should not have to name it in thirteen method signatures to pass
// it along, and a store that does want it is reading transport metadata, which
// is the one thing a context value is for.
//
// The map belongs to the caller and must not be mutated after it is attached.
func WithMemoryContext(ctx context.Context, forwarded map[string]string) context.Context {
	if len(forwarded) == 0 {
		return ctx
	}
	return context.WithValue(ctx, memoryContextKey{}, forwarded)
}

// MemoryContextFrom returns the context a flow forwarded with this call, or nil
// when it forwarded none.
func MemoryContextFrom(ctx context.Context) map[string]string {
	forwarded, _ := ctx.Value(memoryContextKey{}).(map[string]string)
	return forwarded
}

// MemoryContextHeader carries the forwarded agent-memory context to a remote
// store. It is one header holding an encoded map rather than one header per
// entry, because the keys are the flow author's and neither an arbitrary key nor
// an arbitrary value is safe as a header name or as a header value.
//
// It is a credential: it carries whatever a flow chose to forward, which is the
// kind of thing worth forwarding precisely because it is sensitive. It must be
// stripped on a cross-host redirect and must never be logged.
const MemoryContextHeader = "X-Octo-Agent-Context"

// EncodeMemoryContext renders a forwarded context for MemoryContextHeader, and
// returns empty for a context with nothing in it.
func EncodeMemoryContext(forwarded map[string]string) string {
	if len(forwarded) == 0 {
		return ""
	}
	raw, err := json.Marshal(forwarded)
	if err != nil {
		// map[string]string always marshals. Returning empty rather than panicking
		// keeps an impossible failure from taking a run down.
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
