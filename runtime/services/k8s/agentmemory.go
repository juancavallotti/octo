package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
)

// agentMemory is the cluster module's agent-memory store: the orchestrator's
// database, reached over the same deployment-scoped HTTP surface the KV store
// uses, with the same token and the same version header.
//
// The pod sends its deployment id and nothing else, because that is the only
// identity it has. The orchestrator resolves the integration from it, and the
// rows are keyed on the integration — which is why memory survives a redeploy
// where kv_store does not.
type agentMemory struct {
	baseURL      string
	deploymentID string
	token        string
	http         *http.Client

	// unavailable latches when the orchestrator does not serve these routes, so a
	// runtime rolled out ahead of the orchestrator degrades to the older per-thread
	// transcript instead of failing every run. See markUnavailable.
	mu          sync.RWMutex
	unavailable bool
}

// memoryTimeout bounds an ordinary memory request. searchTimeout is longer
// because search is the only operation that can touch every conversation an
// agent has.
const (
	memoryTimeout       = 15 * time.Second
	memorySearchTimeout = 30 * time.Second
)

// newAgentMemory returns the store for a deployment.
func newAgentMemory(baseURL, deploymentID, token string) *agentMemory {
	return &agentMemory{
		baseURL:      strings.TrimRight(baseURL, "/"),
		deploymentID: deploymentID,
		token:        token,
		http:         &http.Client{Timeout: memorySearchTimeout},
	}
}

func (c *agentMemory) close() { c.http.CloseIdleConnections() }

// Enabled reports whether the store is usable. It is false without an
// orchestrator to talk to, and false once the orchestrator has answered 404 —
// see markUnavailable.
func (c *agentMemory) Enabled() bool {
	if c.baseURL == "" || c.deploymentID == "" {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return !c.unavailable
}

// Capabilities reports what the store can do beyond the interface.
//
// Semantic is false here even where the orchestrator has embeddings configured.
// The pod cannot know: whether search ranks semantically is a property of the
// deployment's settings, not of this client, and the only caller that cares is a
// UI reading the orchestrator directly. Search works either way, which is the
// point of it being a capability rather than a second method.
func (c *agentMemory) Capabilities() core.MemoryCapabilities {
	return core.MemoryCapabilities{Semantic: false}
}

// markUnavailable latches the store off after the orchestrator says it does not
// know these routes.
//
// This is what removes the rollout ordering constraint. The runtime and the
// orchestrator are separate images with separate tags, so a pod built with this
// can meet an orchestrator that predates the routes — and the alternative to
// degrading is every agent run failing on a 404 from a store it was told it had.
// Said once, loudly, because the fix is to roll the orchestrator forward.
func (c *agentMemory) markUnavailable() {
	c.mu.Lock()
	first := !c.unavailable
	c.unavailable = true
	c.mu.Unlock()
	if first {
		slog.Warn("orchestrator does not serve agent memory; agents fall back to per-thread transcripts",
			"hint", "roll the orchestrator forward to the version that serves /deployments/{id}/agent-memory")
	}
}

// threadURL builds the URL for one conversation's sub-resource.
func (c *agentMemory) threadURL(ref core.MemoryRef, suffix string) string {
	return fmt.Sprintf("%s/deployments/%s/agent-memory/%s/threads/%s%s",
		c.baseURL,
		url.PathEscape(c.deploymentID),
		url.PathEscape(ref.AgentID),
		url.PathEscape(ref.ThreadKey),
		suffix)
}

// userURL builds the URL for one person's memories under an agent.
func (c *agentMemory) userURL(ref core.MemoryRef, suffix string) string {
	return fmt.Sprintf("%s/deployments/%s/agent-memory/%s/users/%s/memories%s",
		c.baseURL,
		url.PathEscape(c.deploymentID),
		url.PathEscape(ref.AgentID),
		url.PathEscape(ref.UserID),
		suffix)
}

// searchURL builds the search URL for an agent.
func (c *agentMemory) searchURL(agentID string) string {
	return fmt.Sprintf("%s/deployments/%s/agent-memory/%s/search",
		c.baseURL, url.PathEscape(c.deploymentID), url.PathEscape(agentID))
}

// statusError builds an error from an unexpected response, with a short snippet
// of the body for context. It mirrors httpStore.statusError, which is a method on
// that type and so not reachable from here.
func statusError(op string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, errorBodySnippet))
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		return fmt.Errorf("%s: unexpected status %s", op, resp.Status)
	}
	return fmt.Errorf("%s: unexpected status %s: %s", op, resp.Status, msg)
}

// errListingIsPlatformOnly says why a pod cannot enumerate conversations.
var errListingIsPlatformOnly = errors.New(
	"agent memory: listing conversations is an integration-scoped operation; a runtime " +
		"knows only its deployment, and search is the deployment-scoped way to look back")

// LoadWorking reads a conversation's live context.
func (c *agentMemory) LoadWorking(ctx context.Context, ref core.MemoryRef) (core.WorkingMemory, bool, error) {
	//nolint:bodyclose // drainClose (deferred below) closes the body
	resp, err := c.do(ctx, http.MethodGet, c.threadURL(ref, "/working"), nil, nil, memoryTimeout)
	if err != nil {
		return core.WorkingMemory{}, false, err
	}
	defer drainClose(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		payload, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return core.WorkingMemory{}, false, fmt.Errorf("agent memory load: read body: %w", readErr)
		}
		iteration, _ := strconv.Atoi(resp.Header.Get("X-Agent-Iteration"))
		tokens, _ := strconv.Atoi(resp.Header.Get("X-Agent-Tokens"))
		return core.WorkingMemory{
			Version:   parseVersion(resp.Header.Get(headerVersion)),
			Iteration: iteration,
			Tokens:    tokens,
			Payload:   payload,
		}, true, nil
	case http.StatusNotFound:
		// Ambiguous on this one route, and harmlessly so: a conversation with no
		// working memory yet and an orchestrator that does not serve the route both
		// answer 404, and both mean "resume from nothing". The latching happens on the
		// write path, where a 404 has no innocent reading.
		return core.WorkingMemory{}, false, nil
	default:
		return core.WorkingMemory{}, false, statusError("agent memory load", resp)
	}
}

// SaveWorking stores the live context.
func (c *agentMemory) SaveWorking(
	ctx context.Context, ref core.MemoryRef, wm core.WorkingMemory,
) (int64, error) {
	headers := map[string]string{
		headerVersion:       strconv.FormatInt(wm.Version, 10),
		"X-Agent-Iteration": strconv.Itoa(wm.Iteration),
		"X-Agent-Tokens":    strconv.Itoa(wm.Tokens),
		"Content-Type":      "application/octet-stream",
	}
	//nolint:bodyclose // drainClose (deferred below) closes the body
	resp, err := c.do(ctx, http.MethodPut, c.threadURL(ref, "/working"),
		bytes.NewReader(wm.Payload), headers, memoryTimeout)
	if err != nil {
		return 0, err
	}
	defer drainClose(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		return parseVersion(resp.Header.Get(headerVersion)), nil
	case http.StatusConflict:
		return 0, core.ErrVersionConflict
	case http.StatusNotFound:
		c.markUnavailable()
		return 0, core.ErrMemoryDisabled
	default:
		return 0, statusError("agent memory save", resp)
	}
}

// turnWire is one turn on the wire. It mirrors the orchestrator's shape rather
// than core.Turn's, so a field added to one is a deliberate change to both.
type turnWire struct {
	Seq       int64           `json:"seq,omitempty"`
	Role      string          `json:"role"`
	Text      string          `json:"text"`
	Tokens    int             `json:"tokens,omitempty"`
	Attrs     json.RawMessage `json:"attrs,omitempty"`
	CreatedAt time.Time       `json:"createdAt,omitempty"`
}

// AppendTurns records completed turns.
func (c *agentMemory) AppendTurns(
	ctx context.Context, ref core.MemoryRef, turns []core.Turn,
) (int64, error) {
	wire := make([]turnWire, 0, len(turns))
	for _, t := range turns {
		wire = append(wire, turnWire{
			Seq: t.Seq, Role: string(t.Role), Text: t.Text, Tokens: t.Tokens, Attrs: t.Attrs,
		})
	}
	var out struct {
		Version int64 `json:"version"`
	}
	err := c.json(ctx, http.MethodPost, c.threadURL(ref, "/turns"),
		map[string]any{"turns": wire}, &out, memoryTimeout)
	return out.Version, err
}

// ListThreads returns an agent's conversations.
//
// The listing routes are integration-scoped on the orchestrator, so a pod cannot
// reach them: it knows a deployment, not an integration, and giving it a way to
// ask about the integration would hand every pod a listing of every conversation
// the installation has. A flow that wants to look through its own history uses
// Search, which is deployment-scoped and returns matches rather than everything.
func (c *agentMemory) ListThreads(
	_ context.Context, _, _ string, _ core.Page,
) ([]core.Thread, string, error) {
	return nil, "", errListingIsPlatformOnly
}

// ReadThread returns a conversation. Not reachable from a pod, for the same
// reason ListThreads is not.
func (c *agentMemory) ReadThread(
	_ context.Context, _ core.MemoryRef, _ core.Page,
) (core.Thread, []core.Turn, string, error) {
	return core.Thread{}, nil, "", errListingIsPlatformOnly
}

// DeleteThread erases a conversation: its working memory, its turns and the
// conversation itself. This is what clear-agent-memory reaches.
func (c *agentMemory) DeleteThread(ctx context.Context, ref core.MemoryRef) error {
	//nolint:bodyclose // drainClose (deferred by the caller) closes the body
	resp, err := c.do(ctx, http.MethodDelete, c.threadURL(ref, ""), nil, nil, memoryTimeout)
	if err != nil {
		return err
	}
	defer drainClose(resp.Body)
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		return nil
	default:
		return statusError("agent memory delete thread", resp)
	}
}

// SetTitle names a conversation.
func (c *agentMemory) SetTitle(ctx context.Context, ref core.MemoryRef, title string) error {
	return c.json(ctx, http.MethodPut, c.threadURL(ref, "/title"),
		map[string]string{"title": title}, nil, memoryTimeout)
}

// memoryWire is one curated memory on the wire.
type memoryWire struct {
	Name      string    `json:"name"`
	Value     string    `json:"value"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Memories returns what the agent has kept about a person.
func (c *agentMemory) Memories(ctx context.Context, ref core.MemoryRef) ([]core.UserMemory, error) {
	var wire []memoryWire
	if err := c.json(ctx, http.MethodGet, c.userURL(ref, ""), nil, &wire, memoryTimeout); err != nil {
		return nil, err
	}
	out := make([]core.UserMemory, 0, len(wire))
	for _, m := range wire {
		out = append(out, core.UserMemory{
			Name: m.Name, Value: m.Value, Version: m.Version,
			CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
		})
	}
	return out, nil
}

// PutMemory stores or corrects one curated memory.
func (c *agentMemory) PutMemory(
	ctx context.Context, ref core.MemoryRef, name, value string, expectedVersion int64,
) (int64, error) {
	body, err := json.Marshal(map[string]string{"value": value})
	if err != nil {
		return 0, fmt.Errorf("agent memory put: encode: %w", err)
	}
	headers := map[string]string{
		headerVersion:  strconv.FormatInt(expectedVersion, 10),
		"Content-Type": "application/json",
	}
	//nolint:bodyclose // drainClose (deferred below) closes the body
	resp, err := c.do(ctx, http.MethodPut, c.userURL(ref, "/"+url.PathEscape(name)),
		bytes.NewReader(body), headers, memoryTimeout)
	if err != nil {
		return 0, err
	}
	defer drainClose(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		return parseVersion(resp.Header.Get(headerVersion)), nil
	case http.StatusConflict:
		return 0, core.ErrVersionConflict
	default:
		return 0, statusError("agent memory put", resp)
	}
}

// DeleteMemory forgets one curated memory.
func (c *agentMemory) DeleteMemory(ctx context.Context, ref core.MemoryRef, name string) error {
	//nolint:bodyclose // drainClose (deferred by the caller) closes the body
	resp, err := c.do(ctx, http.MethodDelete, c.userURL(ref, "/"+url.PathEscape(name)),
		nil, nil, memoryTimeout)
	if err != nil {
		return err
	}
	defer drainClose(resp.Body)
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		return nil
	default:
		return statusError("agent memory forget", resp)
	}
}

// hitWire is one search result on the wire.
type hitWire struct {
	Kind      string  `json:"kind"`
	ThreadKey string  `json:"threadKey"`
	Name      string  `json:"name"`
	Text      string  `json:"text"`
	Seq       int64   `json:"seq"`
	Score     float64 `json:"score"`
}

// Search looks through the agent's conversations and curated memories.
func (c *agentMemory) Search(ctx context.Context, q core.MemoryQuery) ([]core.MemoryHit, error) {
	var wire []hitWire
	body := map[string]any{
		"userId": q.UserID, "threadKey": q.ThreadKey,
		"text": q.Text, "scope": string(q.Scope), "limit": q.Limit,
	}
	if err := c.json(ctx, http.MethodPost, c.searchURL(q.AgentID), body, &wire, memorySearchTimeout); err != nil {
		return nil, err
	}
	out := make([]core.MemoryHit, 0, len(wire))
	for _, h := range wire {
		out = append(out, core.MemoryHit{
			Kind: h.Kind, ThreadKey: h.ThreadKey, Name: h.Name,
			Text: h.Text, Seq: h.Seq, Score: h.Score,
		})
	}
	return out, nil
}

// do issues one request with the module's bearer token and a per-call deadline.
func (c *agentMemory) do(
	ctx context.Context, method, endpoint string, body io.Reader,
	headers map[string]string, timeout time.Duration,
) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	// The cancel is deliberately not deferred here: the response body is read by
	// the caller, and cancelling the context before that closes it underneath them.
	// It is attached to the body instead, so the deadline lives exactly as long as
	// the response does.
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("agent memory: new request: %w", err)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req) //nolint:bodyclose // the caller closes it; cancel rides along
	if err != nil {
		cancel()
		return nil, fmt.Errorf("agent memory: %w", err)
	}
	resp.Body = &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

// json issues a request with an optional JSON body and decodes an optional JSON
// response.
func (c *agentMemory) json(
	ctx context.Context, method, endpoint string, in, out any, timeout time.Duration,
) error {
	var body io.Reader
	headers := map[string]string{}
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("agent memory: encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
		headers["Content-Type"] = "application/json"
	}
	//nolint:bodyclose // drainClose (deferred by the caller) closes the body
	resp, err := c.do(ctx, method, endpoint, body, headers, timeout)
	if err != nil {
		return err
	}
	defer drainClose(resp.Body)

	switch {
	case resp.StatusCode == http.StatusNotFound:
		c.markUnavailable()
		return core.ErrMemoryDisabled
	case resp.StatusCode == http.StatusConflict:
		return core.ErrVersionConflict
	case resp.StatusCode >= http.StatusBadRequest:
		return statusError("agent memory", resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("agent memory: decode response: %w", err)
	}
	return nil
}

// cancelOnClose ties a request's deadline to the lifetime of its response body.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	if err != nil {
		return fmt.Errorf("agent memory: close response: %w", err)
	}
	return nil
}
