package agentmemory

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// maxIdentifierLen bounds an agent id, thread key, user id or memory name.
//
// These are opaque strings a flow author chooses, and they are stored, indexed
// and put in URLs. A cap keeps an index entry bounded and a path readable; it is
// generous enough that nothing legitimate meets it.
const maxIdentifierLen = 256

// deploymentTTL is how long a deployment's integration is remembered.
//
// The relation is immutable — a deployment belongs to the integration it was
// created for and never moves — so the cache can only be wrong by remembering a
// deployment that has since been deleted, whose writes are going nowhere useful
// anyway. The TTL exists to bound the map, not to chase correctness.
const deploymentTTL = time.Hour

// Deployments resolves a deployment to the integration it belongs to.
type Deployments interface {
	IntegrationID(ctx context.Context, deploymentID string) (string, error)
}

// Store is the database surface a Service needs; *Repo satisfies it.
type Store interface {
	LoadWorking(ctx context.Context, ref Ref) (Working, bool, error)
	SaveWorking(ctx context.Context, ref Ref, w Working) (int64, error)
	AppendTurns(ctx context.Context, ref Ref, turns []Turn) (int64, error)
	ListThreads(ctx context.Context, integrationID, agentID, userID string, page Page) ([]Thread, string, error)
	ReadThread(ctx context.Context, ref Ref, page Page) (Thread, []Turn, string, error)
	DeleteThread(ctx context.Context, ref Ref) error
	SetTitle(ctx context.Context, ref Ref, title string) error
	ListAgents(ctx context.Context, integrationID string) ([]AgentSummary, error)
	Memories(ctx context.Context, ref Ref) ([]UserMemory, error)
	PutMemory(ctx context.Context, ref Ref, name, value string, expectedVersion int64) (int64, error)
	DeleteMemory(ctx context.Context, ref Ref, name string) error
	DeleteForIntegration(ctx context.Context, integrationID string) error
	SearchText(ctx context.Context, integrationID string, q Query) ([]Hit, error)
}

// Service is the operation surface both callers share.
//
// There are two, and they address memory differently on purpose. The runtime
// names a DEPLOYMENT, because that is the only identity a pod has, and this
// resolves it. The platform names an INTEGRATION, because that is what an
// operator is looking at and what the memory actually belongs to. Everything
// below the resolution is the same code.
type Service struct {
	store       Store
	deployments Deployments

	mu     sync.Mutex
	cached map[string]cachedIntegration

	// The optional vector half, nil until an embedding provider is wired. See
	// embedder.go: everything the service does works without it.
	embedder Embedder
	sweeper  Sweeper
}

type cachedIntegration struct {
	id string
	at time.Time
}

// NewService returns a Service over store, resolving deployments through d.
func NewService(store Store, d Deployments) *Service {
	return &Service{store: store, deployments: d, cached: map[string]cachedIntegration{}}
}

// RefForDeployment builds a Ref for a runtime caller, resolving its deployment
// to the integration the memory belongs to.
func (s *Service) RefForDeployment(ctx context.Context, deploymentID, agentID, threadKey, userID string) (Ref, error) {
	integrationID, err := s.integrationFor(ctx, deploymentID)
	if err != nil {
		return Ref{}, err
	}
	return NewRef(integrationID, agentID, threadKey, userID)
}

// NewRef validates the identifiers a Ref is made of.
//
// Refused rather than trimmed or escaped: these are keys, and a key a caller
// reads back has to be the one it wrote. A control character in a thread key is
// a bug in whatever produced it, and silently rewriting it would hide that while
// splitting one conversation into two.
func NewRef(integrationID, agentID, threadKey, userID string) (Ref, error) {
	if strings.TrimSpace(integrationID) == "" {
		return Ref{}, fmt.Errorf("%w: no integration", ErrInvalidRef)
	}
	if err := validIdentifier("agentId", agentID, true); err != nil {
		return Ref{}, err
	}
	if err := validIdentifier("threadKey", threadKey, false); err != nil {
		return Ref{}, err
	}
	if err := validIdentifier("userId", userID, false); err != nil {
		return Ref{}, err
	}
	return Ref{IntegrationID: integrationID, AgentID: agentID, ThreadKey: threadKey, UserID: userID}, nil
}

// validIdentifier rejects a key that cannot be stored. required says whether an
// empty value is allowed — a thread key is empty for a user-memory operation,
// and a user id is empty for an agent that serves no particular person.
func validIdentifier(field, value string, required bool) error {
	if value == "" {
		if required {
			return fmt.Errorf("%w: %s is required", ErrInvalidRef, field)
		}
		return nil
	}
	if len(value) > maxIdentifierLen {
		return fmt.Errorf("%w: %s is longer than %d bytes", ErrInvalidRef, field, maxIdentifierLen)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("%w: %s is not valid UTF-8", ErrInvalidRef, field)
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: %s contains a control character", ErrInvalidRef, field)
		}
	}
	return nil
}

// integrationFor resolves a deployment to its integration, remembering the
// answer. See deploymentTTL for why caching is safe.
func (s *Service) integrationFor(ctx context.Context, deploymentID string) (string, error) {
	s.mu.Lock()
	if hit, ok := s.cached[deploymentID]; ok && time.Since(hit.at) < deploymentTTL {
		s.mu.Unlock()
		return hit.id, nil
	}
	s.mu.Unlock()

	id, err := s.deployments.IntegrationID(ctx, deploymentID)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.cached[deploymentID] = cachedIntegration{id: id, at: time.Now()}
	s.mu.Unlock()
	return id, nil
}

// LoadWorking returns a conversation's live context.
func (s *Service) LoadWorking(ctx context.Context, ref Ref) (Working, bool, error) {
	return s.store.LoadWorking(ctx, ref)
}

// SaveWorking stores the live context.
func (s *Service) SaveWorking(ctx context.Context, ref Ref, w Working) (int64, error) {
	return s.store.SaveWorking(ctx, ref, w)
}

// AppendTurns records completed turns.
func (s *Service) AppendTurns(ctx context.Context, ref Ref, turns []Turn) (int64, error) {
	for i := range turns {
		if turns[i].Role == "" {
			return 0, fmt.Errorf("%w: a turn needs a role", ErrInvalidRef)
		}
	}
	return s.store.AppendTurns(ctx, ref, turns)
}

// ListThreads returns an agent's conversations.
func (s *Service) ListThreads(
	ctx context.Context, integrationID, agentID, userID string, page Page,
) (ThreadPage, error) {
	if err := validIdentifier("agentId", agentID, true); err != nil {
		return ThreadPage{}, err
	}
	threads, next, err := s.store.ListThreads(ctx, integrationID, agentID, userID, page)
	if err != nil {
		return ThreadPage{}, err
	}
	if threads == nil {
		threads = []Thread{}
	}
	return ThreadPage{Threads: threads, Next: next}, nil
}

// ReadThread returns a conversation and a page of its turns.
func (s *Service) ReadThread(ctx context.Context, ref Ref, page Page) (Transcript, error) {
	thread, turns, next, err := s.store.ReadThread(ctx, ref, page)
	if err != nil {
		return Transcript{}, err
	}
	if turns == nil {
		turns = []Turn{}
	}
	return Transcript{Thread: thread, Turns: turns, Next: next}, nil
}

// DeleteThread erases a conversation.
func (s *Service) DeleteThread(ctx context.Context, ref Ref) error {
	return s.store.DeleteThread(ctx, ref)
}

// SetTitle names a conversation. A title longer than a label is trimmed rather
// than refused: unlike a key, nothing addresses a conversation by its title, so
// there is nothing for a caller to read back and compare.
func (s *Service) SetTitle(ctx context.Context, ref Ref, title string) error {
	return s.store.SetTitle(ctx, ref, trimTitle(title))
}

// maxTitleLen bounds a stored title. It is a label in a list.
const maxTitleLen = 200

// trimTitle cuts a title to length on a rune boundary, so a non-ASCII title is
// never stored as an invalid sequence.
func trimTitle(title string) string {
	title = strings.TrimSpace(title)
	if len(title) <= maxTitleLen {
		return title
	}
	cut := maxTitleLen
	for cut > 0 && !utf8.RuneStart(title[cut]) {
		cut--
	}
	return strings.TrimSpace(title[:cut])
}

// ListAgents summarizes which agents have memory under an integration.
func (s *Service) ListAgents(ctx context.Context, integrationID string) ([]AgentSummary, error) {
	agents, err := s.store.ListAgents(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	if agents == nil {
		agents = []AgentSummary{}
	}
	return agents, nil
}

// Memories returns what an agent has kept about one person.
func (s *Service) Memories(ctx context.Context, ref Ref) ([]UserMemory, error) {
	if ref.UserID == "" {
		return nil, fmt.Errorf("%w: userId is required to read user memory", ErrInvalidRef)
	}
	memories, err := s.store.Memories(ctx, ref)
	if err != nil {
		return nil, err
	}
	if memories == nil {
		memories = []UserMemory{}
	}
	return memories, nil
}

// PutMemory stores or corrects one curated memory.
func (s *Service) PutMemory(ctx context.Context, ref Ref, name, value string, expectedVersion int64) (int64, error) {
	if ref.UserID == "" {
		return 0, fmt.Errorf("%w: userId is required to store user memory", ErrInvalidRef)
	}
	if err := validIdentifier("name", name, true); err != nil {
		return 0, err
	}
	if strings.TrimSpace(value) == "" {
		return 0, fmt.Errorf("%w: a memory needs a value", ErrInvalidRef)
	}
	return s.store.PutMemory(ctx, ref, name, value, expectedVersion)
}

// DeleteMemory forgets one curated memory.
func (s *Service) DeleteMemory(ctx context.Context, ref Ref, name string) error {
	if ref.UserID == "" {
		return fmt.Errorf("%w: userId is required to forget user memory", ErrInvalidRef)
	}
	return s.store.DeleteMemory(ctx, ref, name)
}

// DeleteForIntegration removes everything an integration's agents remember, and
// forgets the deployment mappings that pointed at it.
//
// Dropping the cache is what closes the window where a pod still finishing a run
// writes memory for an integration that has just been deleted. It does not close
// it completely — a write already past the lookup still lands, and the row is
// then an orphan — but that is the same bargain logs, traces and kv_store all
// make, and it is why cleanup here is explicit rather than a cascade. Without
// this the window was the cache's whole hour.
func (s *Service) DeleteForIntegration(ctx context.Context, integrationID string) error {
	s.forgetIntegration(integrationID)
	return s.store.DeleteForIntegration(ctx, integrationID)
}

// forgetIntegration drops every cached deployment mapping for an integration.
func (s *Service) forgetIntegration(integrationID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for deployment, hit := range s.cached {
		if hit.id == integrationID {
			delete(s.cached, deployment)
		}
	}
}

// Search returns the memory most relevant to a query.
//
// Semantic when an embedding provider is configured and has something to match
// against, full-text otherwise, and the caller cannot tell which ran. That is why
// Capabilities is a fact about the store rather than two different methods: the
// question a caller asks is the same either way, and mid-backfill the answer can
// come from either index for two searches a second apart.
func (s *Service) Search(ctx context.Context, integrationID string, q Query) ([]Hit, error) {
	if err := validIdentifier("agentId", q.AgentID, true); err != nil {
		return nil, err
	}
	switch q.Scope {
	case ScopeAll, ScopeTurns, ScopeUser:
	default:
		// Refused rather than treated as "search everything": an unrecognised scope is
		// a caller asking for something this does not do, and quietly widening the
		// search is the answer least likely to be what they meant.
		return nil, fmt.Errorf("%w: unknown search scope %q", ErrInvalidRef, q.Scope)
	}
	if strings.TrimSpace(q.Text) == "" {
		return []Hit{}, nil
	}
	if hits, ok := s.searchSemantic(ctx, integrationID, q); ok {
		return hits, nil
	}
	hits, err := s.store.SearchText(ctx, integrationID, q)
	if err != nil {
		return nil, err
	}
	if hits == nil {
		hits = []Hit{}
	}
	return hits, nil
}
