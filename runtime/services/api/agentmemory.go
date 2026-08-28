package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
)

// Headers carrying working memory's metadata beside its opaque payload, the same
// three the k8s module uses.
const (
	headerIteration = "X-Agent-Iteration"
	//nolint:gosec // a header name for a token count, not a credential
	headerTokens = "X-Agent-Tokens"
)

// defaultMaxTurnsPerAppend bounds one append when the platform declares no limit,
// so a long run does not arrive as one enormous request.
const defaultMaxTurnsPerAppend = 100

// agentMemory is the agent-memory store delegated to the platform API.
//
// Unlike the k8s module, listing and reading threads are implemented here. k8s
// refuses them because a pod knows only its deployment and listing is
// integration-scoped — but this module's server is the operator's own system,
// which knows its own tenancy. Discovery gates each, so a platform that would
// rather not expose them says so.
type agentMemory struct {
	c        *client
	latch    *latch
	caps     agentMemoryFeature
	maxTurns int
}

func newAgentMemory(c *client, f agentMemoryFeature) *agentMemory {
	maxTurns := f.MaxTurnsPerAppend
	if maxTurns <= 0 {
		maxTurns = defaultMaxTurnsPerAppend
	}
	return &agentMemory{
		c:        c,
		latch:    &latch{feature: FeatureAgentMemory},
		caps:     f,
		maxTurns: maxTurns,
	}
}

// Enabled answers from discovery and the latch, without a round trip: it is on
// the hot path of every agent run, and the engine branches on it before it does
// anything else.
func (m *agentMemory) Enabled() bool { return m.latch.live() }

// Capabilities reports what the platform said it can do.
func (m *agentMemory) Capabilities() core.MemoryCapabilities {
	return core.MemoryCapabilities{Semantic: m.caps.Semantic}
}

// threadURL addresses one conversation's sub-resource.
//
// The user rides as a query parameter rather than in the path. A conversation is
// addressed by its thread key alone — that is what makes it the same conversation
// on every write — and a user segment would give it a second address under which
// a write naming a different user could mint a duplicate.
//
// It still has to be sent: a platform records who a conversation is with on the
// first write that names one, and omitting it stores every conversation
// attributed to nobody, so an agent keeps a complete history that the person's
// own view of it then shows as empty.
func (m *agentMemory) threadURL(r route, ref core.MemoryRef) string {
	return query(m.c.url(r, ref.AgentID, ref.ThreadKey), "userId", ref.UserID)
}

// LoadWorking reads a conversation's live context.
func (m *agentMemory) LoadWorking(
	ctx context.Context, ref core.MemoryRef,
) (core.WorkingMemory, bool, error) {
	if !m.latch.live() {
		return core.WorkingMemory{}, false, nil
	}
	//nolint:bodyclose // drainClose (deferred below) closes the body
	resp, err := m.c.do(ctx, routeMemoryLoadWorking,
		m.threadURL(routeMemoryLoadWorking, ref), nil, nil, m.c.timeout)
	if err != nil {
		return core.WorkingMemory{}, false, err
	}
	defer drainClose(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		payload, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return core.WorkingMemory{}, false, fmt.Errorf("api agent memory load: read body: %w", readErr)
		}
		return workingFrom(resp, payload), true, nil
	case http.StatusNotFound:
		// Ambiguous on this one route, and harmlessly so: a conversation with no
		// working memory yet and a platform that does not serve the route both
		// answer 404, and both mean "resume from nothing". The latch lives on the
		// write path, where a 404 has no innocent reading.
		return core.WorkingMemory{}, false, nil
	case http.StatusNotImplemented:
		m.latch.mark()
		return core.WorkingMemory{}, false, nil
	default:
		return core.WorkingMemory{}, false, statusError(routeOp(routeMemoryLoadWorking), resp)
	}
}

// workingFrom rebuilds working memory from a response's headers and body.
func workingFrom(resp *http.Response, payload []byte) core.WorkingMemory {
	iteration, _ := strconv.Atoi(resp.Header.Get(headerIteration))
	tokens, _ := strconv.Atoi(resp.Header.Get(headerTokens))
	return core.WorkingMemory{
		Version:   parseVersion(resp.Header.Get(headerVersion)),
		Iteration: iteration,
		Tokens:    tokens,
		Payload:   payload,
	}
}

// SaveWorking stores the live context, creating the thread if it is new.
func (m *agentMemory) SaveWorking(
	ctx context.Context, ref core.MemoryRef, wm core.WorkingMemory,
) (int64, error) {
	if !m.latch.live() {
		return 0, core.ErrMemoryDisabled
	}
	headers := map[string]string{
		headerVersion:     strconv.FormatInt(wm.Version, 10),
		headerIteration:   strconv.Itoa(wm.Iteration),
		headerTokens:      strconv.Itoa(wm.Tokens),
		contentTypeHeader: contentTypeBytes,
	}
	//nolint:bodyclose // drainClose (deferred below) closes the body
	resp, err := m.c.do(ctx, routeMemorySaveWorking,
		m.threadURL(routeMemorySaveWorking, ref), wm.Payload, headers, m.c.timeout)
	if err != nil {
		return 0, err
	}
	defer drainClose(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		return parseVersion(resp.Header.Get(headerVersion)), nil
	case http.StatusConflict:
		return 0, core.ErrVersionConflict
	// A 404 has no innocent reading on a write: saving creates the thread when it
	// is new, so nothing here could legitimately be missing.
	case http.StatusNotFound, http.StatusNotImplemented:
		m.latch.mark()
		return 0, core.ErrMemoryDisabled
	default:
		return 0, statusError(routeOp(routeMemorySaveWorking), resp)
	}
}

// turnWire carries one turn.
//
// createdAt is absent deliberately: a turn is recorded at the moment it
// completes, so the append is the event and the store is the only thing that
// knows when it happened. Sending a clock over the wire is how two
// implementations start disagreeing about it.
type turnWire struct {
	Seq    int64  `json:"seq,omitempty"`
	Role   string `json:"role"`
	Text   string `json:"text"`
	Tokens int    `json:"tokens,omitempty"`
	Attrs  []byte `json:"attrs,omitempty"`
}

type appendRequest struct {
	Turns []turnWire `json:"turns"`
}

type versionResponse struct {
	Version int64 `json:"version"`
}

// AppendTurns records completed turns, chunked to the platform's declared limit.
func (m *agentMemory) AppendTurns(
	ctx context.Context, ref core.MemoryRef, turns []core.Turn,
) (int64, error) {
	if !m.latch.live() {
		return 0, core.ErrMemoryDisabled
	}
	var version int64
	for chunk := range chunks(turns, m.maxTurns) {
		out, err := m.appendChunk(ctx, ref, chunk)
		if err != nil {
			return 0, err
		}
		version = out
	}
	return version, nil
}

// appendChunk sends one batch of turns.
func (m *agentMemory) appendChunk(
	ctx context.Context, ref core.MemoryRef, turns []core.Turn,
) (int64, error) {
	wire := make([]turnWire, 0, len(turns))
	for _, t := range turns {
		wire = append(wire, turnWire{
			Seq: t.Seq, Role: string(t.Role), Text: t.Text, Tokens: t.Tokens, Attrs: t.Attrs,
		})
	}
	var out versionResponse
	err := m.c.json(ctx, routeMemoryAppendTurns, m.threadURL(routeMemoryAppendTurns, ref),
		appendRequest{Turns: wire}, &out, m.c.timeout)
	if err != nil {
		return 0, m.memoryError(err)
	}
	return out.Version, nil
}

// chunks yields slices of at most size.
func chunks[T any](items []T, size int) func(func([]T) bool) {
	return func(yield func([]T) bool) {
		for start := 0; start < len(items); start += size {
			if !yield(items[start:min(start+size, len(items))]) {
				return
			}
		}
	}
}

// threadWire carries a conversation's metadata.
type threadWire struct {
	AgentID        string    `json:"agentId"`
	ThreadKey      string    `json:"threadKey"`
	UserID         string    `json:"userId,omitempty"`
	Title          string    `json:"title,omitempty"`
	Version        int64     `json:"version"`
	TurnCount      int       `json:"turnCount"`
	CreatedAt      time.Time `json:"createdAt"`
	LastActivityAt time.Time `json:"lastActivityAt"`
}

func (t threadWire) core() core.Thread {
	return core.Thread{
		AgentID: t.AgentID, ThreadKey: t.ThreadKey, UserID: t.UserID, Title: t.Title,
		Version: t.Version, TurnCount: t.TurnCount,
		CreatedAt: t.CreatedAt, LastActivityAt: t.LastActivityAt,
	}
}

type listThreadsResponse struct {
	Threads []threadWire `json:"threads"`
	Next    string       `json:"next,omitempty"`
}

// ListThreads returns an agent's conversations.
func (m *agentMemory) ListThreads(
	ctx context.Context, agentID, userID string, page core.Page,
) ([]core.Thread, string, error) {
	if !m.latch.live() {
		return nil, "", nil
	}
	if !m.caps.ListThreads {
		return nil, "", errListingNotOffered
	}
	endpoint := query(m.c.url(routeMemoryListThreads, agentID),
		"userId", userID, "cursor", page.Cursor, "limit", limitParam(page.Limit))

	var out listThreadsResponse
	if err := m.c.json(ctx, routeMemoryListThreads, endpoint, nil, &out, m.c.timeout); err != nil {
		return nil, "", m.memoryError(err)
	}
	rows := make([]core.Thread, 0, len(out.Threads))
	for _, t := range out.Threads {
		rows = append(rows, t.core())
	}
	return rows, out.Next, nil
}

type readThreadResponse struct {
	Thread threadWire `json:"thread"`
	Turns  []turnWire `json:"turns"`
	Next   string     `json:"next,omitempty"`
}

// ReadThread returns a conversation's metadata and a page of its turns.
func (m *agentMemory) ReadThread(
	ctx context.Context, ref core.MemoryRef, page core.Page,
) (core.Thread, []core.Turn, string, error) {
	if !m.latch.live() {
		return core.Thread{}, nil, "", nil
	}
	if !m.caps.ReadThread {
		return core.Thread{}, nil, "", errListingNotOffered
	}
	endpoint := query(m.c.url(routeMemoryReadThread, ref.AgentID, ref.ThreadKey),
		"userId", ref.UserID, "cursor", page.Cursor, "limit", limitParam(page.Limit))

	var out readThreadResponse
	if err := m.c.json(ctx, routeMemoryReadThread, endpoint, nil, &out, m.c.timeout); err != nil {
		return core.Thread{}, nil, "", m.memoryError(err)
	}
	turns := make([]core.Turn, 0, len(out.Turns))
	for _, t := range out.Turns {
		turns = append(turns, core.Turn{
			Seq: t.Seq, Role: core.LLMRole(t.Role), Text: t.Text, Tokens: t.Tokens, Attrs: t.Attrs,
		})
	}
	return out.Thread.core(), turns, out.Next, nil
}

// DeleteThread removes a conversation entirely. A missing one is not an error:
// erasure is the operation that must not report false success, and there is
// nothing left to be wrong about.
func (m *agentMemory) DeleteThread(ctx context.Context, ref core.MemoryRef) error {
	if !m.latch.live() {
		return nil
	}
	err := m.c.json(ctx, routeMemoryDeleteThread,
		m.threadURL(routeMemoryDeleteThread, ref), nil, nil, m.c.timeout)
	if errors.Is(err, errAbsent) {
		return nil
	}
	return m.memoryError(err)
}

type titleRequest struct {
	Title string `json:"title"`
}

// SetTitle names a conversation.
func (m *agentMemory) SetTitle(ctx context.Context, ref core.MemoryRef, title string) error {
	if !m.latch.live() {
		return core.ErrMemoryDisabled
	}
	err := m.c.json(ctx, routeMemorySetTitle, m.threadURL(routeMemorySetTitle, ref),
		titleRequest{Title: title}, nil, m.c.timeout)
	return m.memoryError(err)
}

// memoryWire carries one curated memory.
type memoryWire struct {
	Name      string    `json:"name"`
	Value     string    `json:"value"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type memoriesResponse struct {
	Memories []memoryWire `json:"memories"`
}

// Memories returns what the agent has kept about a person.
func (m *agentMemory) Memories(ctx context.Context, ref core.MemoryRef) ([]core.UserMemory, error) {
	if !m.latch.live() {
		return nil, nil
	}
	var out memoriesResponse
	err := m.c.json(ctx, routeMemoryList, m.c.url(routeMemoryList, ref.AgentID, ref.UserID),
		nil, &out, m.c.timeout)
	if err != nil {
		return nil, m.memoryError(err)
	}
	rows := make([]core.UserMemory, 0, len(out.Memories))
	for _, w := range out.Memories {
		rows = append(rows, core.UserMemory{
			Name: w.Name, Value: w.Value, Version: w.Version,
			CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt,
		})
	}
	return rows, nil
}

type putMemoryRequest struct {
	Value string `json:"value"`
}

// PutMemory creates or updates one curated memory by name.
func (m *agentMemory) PutMemory(
	ctx context.Context, ref core.MemoryRef, name, value string, expectedVersion int64,
) (int64, error) {
	if !m.latch.live() {
		return 0, core.ErrMemoryDisabled
	}
	endpoint := query(m.c.url(routeMemoryPut, ref.AgentID, ref.UserID), "name", name)
	headers := map[string]string{
		headerVersion:     strconv.FormatInt(expectedVersion, 10),
		contentTypeHeader: contentTypeJSON,
	}
	body, err := jsonBody(putMemoryRequest{Value: value})
	if err != nil {
		return 0, err
	}
	//nolint:bodyclose // drainClose (deferred below) closes the body
	resp, err := m.c.do(ctx, routeMemoryPut, endpoint, body, headers, m.c.timeout)
	if err != nil {
		return 0, err
	}
	defer drainClose(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		return parseVersion(resp.Header.Get(headerVersion)), nil
	case http.StatusConflict:
		return 0, core.ErrVersionConflict
	// As on SaveWorking, a 404 has no innocent reading here: a memory that does
	// not exist yet is a create, not a miss.
	case http.StatusNotFound, http.StatusNotImplemented:
		m.latch.mark()
		return 0, core.ErrMemoryDisabled
	default:
		return 0, statusError(routeOp(routeMemoryPut), resp)
	}
}

// DeleteMemory forgets one by name. A missing memory is not an error.
func (m *agentMemory) DeleteMemory(ctx context.Context, ref core.MemoryRef, name string) error {
	if !m.latch.live() {
		return nil
	}
	endpoint := query(m.c.url(routeMemoryDelete, ref.AgentID, ref.UserID), "name", name)
	err := m.c.json(ctx, routeMemoryDelete, endpoint, nil, nil, m.c.timeout)
	if errors.Is(err, errAbsent) {
		return nil
	}
	return m.memoryError(err)
}

type searchRequest struct {
	UserID    string `json:"userId,omitempty"`
	ThreadKey string `json:"threadKey,omitempty"`
	Text      string `json:"text"`
	Scope     string `json:"scope,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

type searchResponse struct {
	Hits []hitWire `json:"hits"`
}

type hitWire struct {
	Kind      string  `json:"kind"`
	ThreadKey string  `json:"threadKey,omitempty"`
	Name      string  `json:"name,omitempty"`
	Text      string  `json:"text"`
	Seq       int64   `json:"seq,omitempty"`
	Score     float64 `json:"score,omitempty"`
}

// Search looks through the agent's conversations and curated memories.
func (m *agentMemory) Search(ctx context.Context, q core.MemoryQuery) ([]core.MemoryHit, error) {
	if !m.latch.live() {
		return nil, nil
	}
	if !m.caps.Search {
		return nil, nil
	}
	in := searchRequest{
		UserID: q.UserID, ThreadKey: q.ThreadKey, Text: q.Text,
		Scope: string(q.Scope), Limit: q.Limit,
	}
	var out searchResponse
	// Search is the one operation that can touch every conversation an agent has,
	// so it gets the long timeout.
	err := m.c.json(ctx, routeMemorySearch, m.c.url(routeMemorySearch, q.AgentID),
		in, &out, m.c.longTimeout)
	if err != nil {
		return nil, m.memoryError(err)
	}
	hits := make([]core.MemoryHit, 0, len(out.Hits))
	for _, h := range out.Hits {
		hits = append(hits, core.MemoryHit{
			Kind: h.Kind, ThreadKey: h.ThreadKey, Name: h.Name,
			Text: h.Text, Seq: h.Seq, Score: h.Score,
		})
	}
	return hits, nil
}

// memoryError latches the store off on a 501 and translates it to the sentinel
// the engine already handles.
func (m *agentMemory) memoryError(err error) error {
	if isNotImplemented(err) {
		m.latch.mark()
		return core.ErrMemoryDisabled
	}
	return err
}

// errListingNotOffered says why a listing did not happen when the platform can
// otherwise store memory.
var errListingNotOffered = errors.New(
	"agent memory: this platform API does not offer conversation listing; declare " +
		"\"listThreads\" and \"readThread\" in the discovery document once it does")

// limitParam renders a page limit, omitting a zero so the platform applies its own
// default rather than being asked for none.
func limitParam(limit int) string {
	if limit <= 0 {
		return ""
	}
	return strconv.Itoa(limit)
}
