package agentmemory

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
)

const (
	// requestTimeout bounds the database work behind one request. Search gets
	// longer: it is the only route that can touch every conversation an agent has.
	requestTimeout = 10 * time.Second
	searchTimeout  = 30 * time.Second
	// maxPayloadBytes caps a stored working-memory payload. It is a whole
	// transcript, so the cap is far above the kv store's — but it is still a cap,
	// because nothing should be able to make the orchestrator read without bound.
	maxPayloadBytes = 32 << 20
	// headerVersion carries the object version in both directions, matching the kv
	// routes so the runtime's client is the same shape either way.
	headerVersion = "X-Object-Version"
)

// Handler serves both route families.
//
// They are split by who is asking, not by what they do. The runtime addresses a
// DEPLOYMENT — the only identity a pod has, and one it already authenticates
// with — and the platform addresses an INTEGRATION, which is what an operator is
// looking at and what the memory belongs to. Underneath they are the same
// service; the difference is one resolution step and which surface is read-only.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler serving svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches both route families to mux.
//
// Every pattern is written out in full rather than composed from a shared
// prefix. That is not style: openapi's coverage test scans these sources for the
// literal a route is registered with, so a route assembled by concatenation is
// one the test cannot see — and its whole job is catching a route that was added
// without being described.
func (h *Handler) Register(mux *http.ServeMux) {
	// Runtime-facing, deployment-scoped: the only identity a pod has, and the one
	// it already authenticates with.
	mux.HandleFunc("GET /deployments/{id}/agent-memory/{agentId}/threads/{threadKey}/working", h.getWorking)
	mux.HandleFunc("PUT /deployments/{id}/agent-memory/{agentId}/threads/{threadKey}/working", h.putWorking)
	mux.HandleFunc("POST /deployments/{id}/agent-memory/{agentId}/threads/{threadKey}/turns", h.postTurns)
	mux.HandleFunc("GET /deployments/{id}/agent-memory/{agentId}/threads/{threadKey}", h.getRuntimeThread)
	mux.HandleFunc("PUT /deployments/{id}/agent-memory/{agentId}/threads/{threadKey}/title", h.putRuntimeTitle)
	mux.HandleFunc("DELETE /deployments/{id}/agent-memory/{agentId}/threads/{threadKey}", h.deleteRuntimeThread)
	mux.HandleFunc("GET /deployments/{id}/agent-memory/{agentId}/users/{userId}/memories", h.getRuntimeMemories)
	mux.HandleFunc("PUT /deployments/{id}/agent-memory/{agentId}/users/{userId}/memories/{name}", h.putRuntimeMemory)
	mux.HandleFunc("DELETE /deployments/{id}/agent-memory/{agentId}/users/{userId}/memories/{name}", h.deleteRuntimeMemory)
	mux.HandleFunc("POST /deployments/{id}/agent-memory/{agentId}/search", h.postRuntimeSearch)

	// Platform-facing, integration-scoped: what the chat panel and the admin
	// viewer read, and what the memory actually belongs to.
	mux.HandleFunc("GET /integrations/{id}/agent-memory/agents", h.listAgents)
	mux.HandleFunc("GET /integrations/{id}/agent-memory/{agentId}/threads", h.listThreads)
	mux.HandleFunc("GET /integrations/{id}/agent-memory/{agentId}/threads/{threadKey}", h.readThread)
	mux.HandleFunc("GET /integrations/{id}/agent-memory/{agentId}/threads/{threadKey}/working", h.readWorking)
	mux.HandleFunc("PUT /integrations/{id}/agent-memory/{agentId}/threads/{threadKey}/title", h.putTitle)
	mux.HandleFunc("DELETE /integrations/{id}/agent-memory/{agentId}/threads/{threadKey}", h.deleteThread)
	mux.HandleFunc("GET /integrations/{id}/agent-memory/{agentId}/users/{userId}/memories", h.listMemories)
	mux.HandleFunc("DELETE /integrations/{id}/agent-memory/{agentId}/users/{userId}/memories/{name}", h.deleteMemory)
	mux.HandleFunc("POST /integrations/{id}/agent-memory/{agentId}/search", h.postSearch)
}

// runtimeRef resolves a runtime request's path into a Ref, writing the error
// response itself and reporting whether the caller should carry on.
func (h *Handler) runtimeRef(w http.ResponseWriter, r *http.Request, ctx context.Context) (Ref, bool) {
	ref, err := h.svc.RefForDeployment(ctx,
		r.PathValue("id"), r.PathValue("agentId"), r.PathValue("threadKey"), runtimeUser(r))
	if err != nil {
		h.writeError(w, err)
		return Ref{}, false
	}
	return ref, true
}

// runtimeUser reads who a runtime write is on behalf of.
//
// The user-memory routes address a person in the path, because there the person
// IS the resource. The thread routes do not: a conversation is addressed by its
// thread key, and putting the user in the path too would give one conversation
// two URLs and let a second write under a different user mint a duplicate of it.
// So on those routes the user arrives as a query parameter — an attribute of the
// write rather than part of the address — and the path value wins wherever there
// is one.
//
// It has to arrive somehow. Without it every conversation was stored attributed
// to nobody, and the platform lists a person's conversations BY that attribution,
// so an agent recorded a full history that its own chat panel then showed as
// empty.
func runtimeUser(r *http.Request) string {
	if user := r.PathValue("userId"); user != "" {
		return user
	}
	return r.URL.Query().Get("userId")
}

// platformRef builds a Ref from an integration-scoped path.
func (h *Handler) platformRef(w http.ResponseWriter, r *http.Request) (Ref, bool) {
	ref, err := NewRef(r.PathValue("id"), r.PathValue("agentId"),
		r.PathValue("threadKey"), r.PathValue("userId"))
	if err != nil {
		h.writeError(w, err)
		return Ref{}, false
	}
	return ref, true
}

// getWorking godoc
//
//	@Summary		Read an agent's working memory
//	@Description	The live context an interrupted run resumes from. The body is the runtime's own
//	@Description	serialized transcript — opaque bytes, not JSON — and the current version comes back
//	@Description	in X-Object-Version.
//	@Tags			agent-memory
//	@Produce		octet-stream
//	@Param			id			path		string	true	"Deployment id"
//	@Param			agentId		path		string	true	"Agent id"
//	@Param			threadKey	path		string	true	"Conversation thread key"
//	@Success		200			{string}	string	"the working memory payload"
//	@Header			200			{integer}	X-Object-Version	"the version to send back on a conditional write"
//	@Failure		404			"no working memory for this conversation"
//	@Router			/deployments/{id}/agent-memory/{agentId}/threads/{threadKey}/working [get]
func (h *Handler) getWorking(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	ref, ok := h.runtimeRef(w, r, ctx)
	if !ok {
		return
	}
	working, found, err := h.svc.LoadWorking(ctx, ref)
	if err != nil {
		h.writeError(w, err)
		return
	}
	if !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set(headerVersion, strconv.FormatInt(working.Version, 10))
	w.Header().Set("X-Agent-Iteration", strconv.Itoa(working.Iteration))
	w.Header().Set("X-Agent-Tokens", strconv.Itoa(working.Tokens))
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(working.Payload)
}

// putWorking godoc
//
//	@Summary		Write an agent's working memory
//	@Description	The body is the raw payload. Optimistic concurrency is by header: send
//	@Description	X-Object-Version with the version you last read, or omit it to create. A write
//	@Description	against a version something else has moved past is refused with 409.
//	@Tags			agent-memory
//	@Accept			octet-stream
//	@Param			id					path	string	true	"Deployment id"
//	@Param			agentId				path	string	true	"Agent id"
//	@Param			threadKey			path	string	true	"Conversation thread key"
//	@Param			X-Object-Version	header	integer	false	"Expected version; omit to create"
//	@Success		200					"written"
//	@Header			200					{integer}	X-Object-Version	"the version the write produced"
//	@Failure		409					{object}	httpx.ErrorResponse	"the version did not match"
//	@Failure		413					{object}	httpx.ErrorResponse	"the payload is too large"
//	@Router			/deployments/{id}/agent-memory/{agentId}/threads/{threadKey}/working [put]
func (h *Handler) putWorking(w http.ResponseWriter, r *http.Request) {
	expected, err := versionHeader(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid "+headerVersion+" header")
		return
	}
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPayloadBytes))
	if err != nil {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "working memory payload too large")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	ref, ok := h.runtimeRef(w, r, ctx)
	if !ok {
		return
	}
	iteration, _ := strconv.Atoi(r.Header.Get("X-Agent-Iteration"))
	tokens, _ := strconv.Atoi(r.Header.Get("X-Agent-Tokens"))
	version, err := h.svc.SaveWorking(ctx, ref, Working{
		Version: expected, Iteration: iteration, Tokens: tokens, Payload: payload,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set(headerVersion, strconv.FormatInt(version, 10))
	w.WriteHeader(http.StatusOK)
}

// turnsRequest is the body of a turn append.
type turnsRequest struct {
	Turns []Turn `json:"turns"`
}

// postTurns godoc
//
//	@Summary		Append completed turns to a conversation
//	@Description	The durable record, which is never compacted — unlike working memory, which is.
//	@Description	Sequence numbers are assigned by the server, so two replicas of one agent
//	@Description	interleave rather than collide, which is why no expected version is taken.
//	@Tags			agent-memory
//	@Accept			json
//	@Produce		json
//	@Param			id			path	string			true	"Deployment id"
//	@Param			agentId		path	string			true	"Agent id"
//	@Param			threadKey	path	string			true	"Conversation thread key"
//	@Param			userId		query	string			false	"Who the conversation is with; attributed on first write"
//	@Param			body		body	turnsRequest	true	"The turns to append"
//	@Success		200			{object}	map[string]int64	"the conversation's new version"
//	@Router			/deployments/{id}/agent-memory/{agentId}/threads/{threadKey}/turns [post]
func (h *Handler) postTurns(w http.ResponseWriter, r *http.Request) {
	var body turnsRequest
	if err := httpx.DecodeJSON(w, r, &body); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	ref, ok := h.runtimeRef(w, r, ctx)
	if !ok {
		return
	}
	version, err := h.svc.AppendTurns(ctx, ref, body.Turns)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]int64{"version": version})
}

// titleRequest is the body of a rename.
type titleRequest struct {
	Title string `json:"title"`
}

// getRuntimeThread godoc
//
//	@Summary		Read a conversation's metadata (runtime)
//	@Description	Metadata only — the title, the turn count, when it was last active. The transcript
//	@Description	is not served here: a pod knows a deployment rather than an integration, and a
//	@Description	route that handed it whole conversations would be one every pod could read every
//	@Description	conversation through. This exists so a flow can tell an unnamed conversation from a
//	@Description	named one without paying a model call to name it twice.
//	@Tags			agent-memory
//	@Produce		json
//	@Param			id			path		string	true	"Deployment id"
//	@Param			agentId		path		string	true	"Agent id"
//	@Param			threadKey	path		string	true	"Conversation thread key"
//	@Success		200			{object}	Thread
//	@Failure		404			{object}	httpx.ErrorResponse	"no such conversation"
//	@Router			/deployments/{id}/agent-memory/{agentId}/threads/{threadKey} [get]
func (h *Handler) getRuntimeThread(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	ref, ok := h.runtimeRef(w, r, ctx)
	if !ok {
		return
	}
	// A zero-limit page: the thread row is wanted and the turns are not.
	transcript, err := h.svc.ReadThread(ctx, ref, Page{Limit: 1})
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, transcript.Thread)
}

// putRuntimeTitle godoc
//
//	@Summary	Name a conversation (runtime)
//	@Tags		agent-memory
//	@Accept		json
//	@Param		id			path	string			true	"Deployment id"
//	@Param		agentId		path	string			true	"Agent id"
//	@Param		threadKey	path	string			true	"Conversation thread key"
//	@Param		body		body	titleRequest	true	"The title"
//	@Success	204			"named"
//	@Router		/deployments/{id}/agent-memory/{agentId}/threads/{threadKey}/title [put]
func (h *Handler) putRuntimeTitle(w http.ResponseWriter, r *http.Request) {
	var body titleRequest
	if err := httpx.DecodeJSON(w, r, &body); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	ref, ok := h.runtimeRef(w, r, ctx)
	if !ok {
		return
	}
	if err := h.svc.SetTitle(ctx, ref, body.Title); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteRuntimeThread godoc
//
//	@Summary		Erase a conversation (runtime)
//	@Description	Everything: the working memory, the recorded turns and the conversation itself.
//	@Description	This is what clear-agent-memory reaches, so a partial delete would report success
//	@Description	over a readable copy of what somebody asked to be rid of.
//	@Tags			agent-memory
//	@Param			id			path	string	true	"Deployment id"
//	@Param			agentId		path	string	true	"Agent id"
//	@Param			threadKey	path	string	true	"Conversation thread key"
//	@Success		204			"erased"
//	@Router			/deployments/{id}/agent-memory/{agentId}/threads/{threadKey} [delete]
func (h *Handler) deleteRuntimeThread(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	ref, ok := h.runtimeRef(w, r, ctx)
	if !ok {
		return
	}
	if err := h.svc.DeleteThread(ctx, ref); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// getRuntimeMemories godoc
//
//	@Summary	Read what an agent remembers about a person (runtime)
//	@Tags		agent-memory
//	@Produce	json
//	@Param		id		path	string	true	"Deployment id"
//	@Param		agentId	path	string	true	"Agent id"
//	@Param		userId	path	string	true	"User id"
//	@Success	200		{array}	UserMemory
//	@Router		/deployments/{id}/agent-memory/{agentId}/users/{userId}/memories [get]
func (h *Handler) getRuntimeMemories(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	ref, ok := h.runtimeRef(w, r, ctx)
	if !ok {
		return
	}
	memories, err := h.svc.Memories(ctx, ref)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, memories)
}

// memoryRequest is the body of a user-memory write.
type memoryRequest struct {
	Value string `json:"value"`
}

// putRuntimeMemory godoc
//
//	@Summary		Store or correct one curated memory (runtime)
//	@Description	Optimistic concurrency by X-Object-Version, as for working memory: omit it to
//	@Description	create, send the version you read to correct.
//	@Tags			agent-memory
//	@Accept			json
//	@Produce		json
//	@Param			id					path	string			true	"Deployment id"
//	@Param			agentId				path	string			true	"Agent id"
//	@Param			userId				path	string			true	"User id"
//	@Param			name				path	string			true	"The memory's name"
//	@Param			X-Object-Version	header	integer			false	"Expected version; omit to create"
//	@Param			body				body	memoryRequest	true	"The memory's value"
//	@Success		200					{object}	map[string]int64	"the new version"
//	@Failure		409					{object}	httpx.ErrorResponse	"the version did not match"
//	@Router			/deployments/{id}/agent-memory/{agentId}/users/{userId}/memories/{name} [put]
func (h *Handler) putRuntimeMemory(w http.ResponseWriter, r *http.Request) {
	expected, err := versionHeader(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid "+headerVersion+" header")
		return
	}
	var body memoryRequest
	if err := httpx.DecodeJSON(w, r, &body); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	ref, ok := h.runtimeRef(w, r, ctx)
	if !ok {
		return
	}
	version, err := h.svc.PutMemory(ctx, ref, r.PathValue("name"), body.Value, expected)
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set(headerVersion, strconv.FormatInt(version, 10))
	httpx.WriteJSON(w, http.StatusOK, map[string]int64{"version": version})
}

// deleteRuntimeMemory godoc
//
//	@Summary	Forget one curated memory (runtime)
//	@Tags		agent-memory
//	@Param		id		path	string	true	"Deployment id"
//	@Param		agentId	path	string	true	"Agent id"
//	@Param		userId	path	string	true	"User id"
//	@Param		name	path	string	true	"The memory's name"
//	@Success	204		"forgotten"
//	@Router		/deployments/{id}/agent-memory/{agentId}/users/{userId}/memories/{name} [delete]
func (h *Handler) deleteRuntimeMemory(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	ref, ok := h.runtimeRef(w, r, ctx)
	if !ok {
		return
	}
	if err := h.svc.DeleteMemory(ctx, ref, r.PathValue("name")); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// postRuntimeSearch godoc
//
//	@Summary		Search an agent's memory (runtime)
//	@Description	Conversations and curated memories. Full-text today; semantic ranking replaces the
//	@Description	ranking, not the surface, when an embedding provider is configured.
//	@Tags			agent-memory
//	@Accept			json
//	@Produce		json
//	@Param			id		path	string	true	"Deployment id"
//	@Param			agentId	path	string	true	"Agent id"
//	@Param			body	body	Query	true	"The query"
//	@Success		200		{array}	Hit
//	@Router			/deployments/{id}/agent-memory/{agentId}/search [post]
func (h *Handler) postRuntimeSearch(w http.ResponseWriter, r *http.Request) {
	var q Query
	if err := httpx.DecodeJSON(w, r, &q); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), searchTimeout)
	defer cancel()

	q.AgentID = r.PathValue("agentId")
	integrationID, err := h.svc.integrationFor(ctx, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	hits, err := h.svc.Search(ctx, integrationID, q)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, hits)
}

// listAgents godoc
//
//	@Summary		List the agents that have memory under an integration
//	@Description	So an operator has something to pick from without knowing the ids in advance.
//	@Tags			agent-memory
//	@Produce		json
//	@Param			id	path	string	true	"Integration id"
//	@Success		200	{array}	AgentSummary
//	@Router			/integrations/{id}/agent-memory/agents [get]
func (h *Handler) listAgents(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	agents, err := h.svc.ListAgents(ctx, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, agents)
}

// listThreads godoc
//
//	@Summary		List an agent's conversations
//	@Description	Most recently active first. Paging is a keyset cursor, not an offset: writing to a
//	@Description	conversation is exactly what reorders this listing, so an offset would skip rows
//	@Description	constantly rather than rarely.
//	@Tags			agent-memory
//	@Produce		json
//	@Param			id		path		string	true	"Integration id"
//	@Param			agentId	path		string	true	"Agent id"
//	@Param			userId	query		string	false	"Only this person's conversations"
//	@Param			cursor	query		string	false	"Continue a previous page"
//	@Param			limit	query		integer	false	"Page size"
//	@Success		200		{object}	ThreadPage
//	@Router			/integrations/{id}/agent-memory/{agentId}/threads [get]
func (h *Handler) listThreads(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	page, err := h.svc.ListThreads(ctx, r.PathValue("id"), r.PathValue("agentId"),
		r.URL.Query().Get("userId"), pageFrom(r))
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

// readThread godoc
//
//	@Summary		Read a conversation
//	@Description	The durable record: what was asked and what was answered, in order and uncompacted.
//	@Tags			agent-memory
//	@Produce		json
//	@Param			id			path		string	true	"Integration id"
//	@Param			agentId		path		string	true	"Agent id"
//	@Param			threadKey	path		string	true	"Conversation thread key"
//	@Param			cursor		query		string	false	"Continue a previous page"
//	@Param			limit		query		integer	false	"Page size"
//	@Success		200			{object}	Transcript
//	@Failure		404			{object}	httpx.ErrorResponse	"no such conversation"
//	@Router			/integrations/{id}/agent-memory/{agentId}/threads/{threadKey} [get]
func (h *Handler) readThread(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	ref, ok := h.platformRef(w, r)
	if !ok {
		return
	}
	transcript, err := h.svc.ReadThread(ctx, ref, pageFrom(r))
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, transcript)
}

// workingResponse is the live context, described rather than handed over raw.
//
// The payload is the RUNTIME's serialized transcript and this package has never
// parsed it — Working.Payload is []byte on purpose, so the engine can change the
// format without a migration here. So the wire type says what the store knows
// (how big, how far in, how many tokens, when) and passes the bytes along as text
// for a viewer to make what it can of.
//
// Text and not base64: in practice the runtime writes JSON, and a viewer whose
// whole job is showing an operator what the agent is carrying should not have to
// decode a wrapper to do it. Non-UTF-8 payloads are described and withheld rather
// than mangled, which is the honest answer for a format this route does not own.
type workingResponse struct {
	Working
	// Found distinguishes "this conversation carries no live context" from "there
	// is one and here it is". A conversation that ended cleanly has its transcript
	// and nothing to resume from, which is ordinary rather than an error — so this
	// route answers 200 either way and says which, rather than making every caller
	// treat a 404 as a success it has to recognize.
	Found bool `json:"found"`
	// Bytes is the payload's real size, which is meaningful even when the payload
	// itself is not served.
	Bytes int `json:"bytes"`
	// Payload is the runtime's transcript verbatim. Empty when it is not text.
	Payload string `json:"payload,omitempty"`
	// Readable says which of those two happened, so a viewer can tell an empty
	// working memory from one it was not given.
	Readable bool `json:"readable"`
}

// readWorking godoc
//
//	@Summary		Read a conversation's working memory
//	@Description	The live context an interrupted run would resume from — the compacted, pruned
//	@Description	working copy, as opposed to the durable transcript this conversation's turns hold.
//	@Description	The payload is the runtime's own serialized form and is served as-is: the
//	@Description	orchestrator stores it without parsing it, so that the engine can change the format
//	@Description	without a schema migration.
//	@Tags			agent-memory
//	@Produce		json
//	@Param			id			path		string	true	"Integration id"
//	@Param			agentId		path		string	true	"Agent id"
//	@Param			threadKey	path		string	true	"Conversation thread key"
//	@Description	Answers 200 whether or not there is one: a conversation that ended cleanly has its
//	@Description	transcript and no live context, which is ordinary. Check `found`.
//	@Success		200			{object}	workingResponse
//	@Router			/integrations/{id}/agent-memory/{agentId}/threads/{threadKey}/working [get]
func (h *Handler) readWorking(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	ref, ok := h.platformRef(w, r)
	if !ok {
		return
	}
	working, found, err := h.svc.LoadWorking(ctx, ref)
	if err != nil {
		h.writeError(w, err)
		return
	}
	if !found {
		httpx.WriteJSON(w, http.StatusOK, workingResponse{})
		return
	}
	out := workingResponse{Working: working, Found: true, Bytes: len(working.Payload)}
	if utf8.Valid(working.Payload) {
		out.Payload, out.Readable = string(working.Payload), true
	}
	// The bytes are in the wire type now; sending them twice would double a
	// payload that is measured in hundreds of kilobytes.
	out.Working.Payload = nil
	httpx.WriteJSON(w, http.StatusOK, out)
}

// putTitle godoc
//
//	@Summary		Name a conversation
//	@Description	The runtime labels a new conversation with its opening line. A better title is a
//	@Description	judgement about what the exchange was about — a model call — so it belongs to
//	@Description	whoever wants to pay for it, through this route.
//	@Tags			agent-memory
//	@Accept			json
//	@Param			id			path	string			true	"Integration id"
//	@Param			agentId		path	string			true	"Agent id"
//	@Param			threadKey	path	string			true	"Conversation thread key"
//	@Param			body		body	titleRequest	true	"The title"
//	@Success		204			"named"
//	@Router			/integrations/{id}/agent-memory/{agentId}/threads/{threadKey}/title [put]
func (h *Handler) putTitle(w http.ResponseWriter, r *http.Request) {
	var body titleRequest
	if err := httpx.DecodeJSON(w, r, &body); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	ref, ok := h.platformRef(w, r)
	if !ok {
		return
	}
	if err := h.svc.SetTitle(ctx, ref, body.Title); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteThread godoc
//
//	@Summary		Erase a conversation
//	@Description	Everything: working memory, recorded turns and the conversation itself.
//	@Tags			agent-memory
//	@Param			id			path	string	true	"Integration id"
//	@Param			agentId		path	string	true	"Agent id"
//	@Param			threadKey	path	string	true	"Conversation thread key"
//	@Success		204			"erased"
//	@Router			/integrations/{id}/agent-memory/{agentId}/threads/{threadKey} [delete]
func (h *Handler) deleteThread(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	ref, ok := h.platformRef(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteThread(ctx, ref); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listMemories godoc
//
//	@Summary	Read what an agent remembers about a person
//	@Tags		agent-memory
//	@Produce	json
//	@Param		id		path	string	true	"Integration id"
//	@Param		agentId	path	string	true	"Agent id"
//	@Param		userId	path	string	true	"User id"
//	@Success	200		{array}	UserMemory
//	@Router		/integrations/{id}/agent-memory/{agentId}/users/{userId}/memories [get]
func (h *Handler) listMemories(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	ref, ok := h.platformRef(w, r)
	if !ok {
		return
	}
	memories, err := h.svc.Memories(ctx, ref)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, memories)
}

// deleteMemory godoc
//
//	@Summary		Forget one curated memory
//	@Description	There is deliberately no route to EDIT one. An operator rewriting what an agent
//	@Description	believes about a person, with no audit trail, is a feature that should be asked for
//	@Description	explicitly rather than fall out of a viewer.
//	@Tags			agent-memory
//	@Param			id		path	string	true	"Integration id"
//	@Param			agentId	path	string	true	"Agent id"
//	@Param			userId	path	string	true	"User id"
//	@Param			name	path	string	true	"The memory's name"
//	@Success		204		"forgotten"
//	@Router			/integrations/{id}/agent-memory/{agentId}/users/{userId}/memories/{name} [delete]
func (h *Handler) deleteMemory(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	ref, ok := h.platformRef(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteMemory(ctx, ref, r.PathValue("name")); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// postSearch godoc
//
//	@Summary	Search an agent's memory
//	@Tags		agent-memory
//	@Accept		json
//	@Produce	json
//	@Param		id		path	string	true	"Integration id"
//	@Param		agentId	path	string	true	"Agent id"
//	@Param		body	body	Query	true	"The query"
//	@Success	200		{array}	Hit
//	@Router		/integrations/{id}/agent-memory/{agentId}/search [post]
func (h *Handler) postSearch(w http.ResponseWriter, r *http.Request) {
	var q Query
	if err := httpx.DecodeJSON(w, r, &q); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), searchTimeout)
	defer cancel()

	q.AgentID = r.PathValue("agentId")
	hits, err := h.svc.Search(ctx, r.PathValue("id"), q)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, hits)
}

// pageFrom reads the cursor and limit a listing request carries.
func pageFrom(r *http.Request) Page {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	return Page{Cursor: r.URL.Query().Get("cursor"), Limit: limit}
}

// versionHeader reads the optional expected version. An absent header is 0,
// which means "create".
func versionHeader(r *http.Request) (int64, error) {
	raw := r.Header.Get(headerVersion)
	if raw == "" {
		return 0, nil
	}
	return strconv.ParseInt(raw, 10, 64)
}

// writeError maps the package's sentinels onto status codes. Anything else is a
// server error, and its detail stays in the log rather than the response.
func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrVersionConflict):
		httpx.WriteError(w, http.StatusConflict, "version conflict")
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrInvalidRef):
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
	default:
		httpx.WriteError(w, http.StatusInternalServerError, "agent memory unavailable")
	}
}
