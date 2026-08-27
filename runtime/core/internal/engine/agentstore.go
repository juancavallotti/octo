// First-class agent memory: what an ai-agent does when it has been given an
// agentId and the runtime has a store to put things in.
//
// This sits beside agentmemory.go rather than replacing it. That file is the
// older arrangement — one compacted transcript per thread, in KV — and it is
// still the whole story for an agent with no agentId. The two differ in what
// they promise:
//
//	agentmemory.go   one blob, compacted to a budget, keyed by thread alone
//	this file        working memory AND an uncompacted turn record AND user
//	                 memory, keyed by (agent, thread), versioned, listable
//
// The split that matters is between working memory and history. Working memory
// is what the model re-reads, so it is pruned or summarized to fit a budget;
// history is what a person re-reads, so it is never touched again once written.
// Storing one thing and calling it both is what made "the agent summarized its
// context" and "the conversation is gone" the same event.
//
// Why agentId is opt-in rather than derived: see the AgentID doc comment in
// runtime/types/flow.go. Short version — a derived name is a position in a file,
// and renaming a block would destroy the conversations stored under it.
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// History settings for the ai-agent block.
const (
	historyRecord = "record" // append completed turns to durable history (default)
	historyOff    = "off"    // keep working memory, record nothing
)

// checkpointInterval and checkpointBytes debounce working-memory checkpoints.
//
// The design called for a save at the end of every ReAct iteration, and taken
// literally that is a synchronous round trip to the store inserted into the
// critical path of an answer somebody is watching arrive. Nothing reads working
// memory between iterations — its readers are crash recovery and live
// inspection — so a checkpoint that is a couple of seconds or one large tool
// result behind is worth exactly as much as one that is not, and costs a great
// deal less.
//
// The terminal paths still flush synchronously, so what is stored when a run
// ends is always the run's final state. Only the intermediate checkpoints are
// coalesced, and losing one of those costs nothing: the next overwrites it.
const (
	checkpointInterval = 2 * time.Second
	checkpointBytes    = 64 << 10
)

// titleMaxLen bounds the title minted from a conversation's first turn. It is a
// label in a list, not a summary.
const titleMaxLen = 80

// preambleMaxMemories and preambleMaxChars bound what the memory preamble puts
// in front of every request.
//
// Unbounded, this grows without limit and is spent on every turn of every run:
// an agent that has been talking to somebody for a year would arrive at the
// provider's window before it had read a word of the conversation. The cap is
// per-request rather than on what is stored, because forgetting something on the
// agent's behalf is not the runtime's call — what does not fit is still there,
// and search_memory is how the model reaches it.
const (
	preambleMaxMemories = 50
	preambleMaxChars    = 8 << 10
)

// turnAttrs is the JSON an engine attaches to a stored turn: what it does not
// say in its text. Unanswered marks a question the run never got back to, which
// is worth keeping — somebody asked it.
type turnAttrs struct {
	Iterations int    `json:"iterations,omitempty"`
	Unanswered bool   `json:"unanswered,omitempty"`
	StopReason string `json:"stopReason,omitempty"`
}

// memorySession is one run's view of the agent's memory.
//
// It exists because the version of a working-memory object is per-run state and
// aiAgent is shared by every message the block handles at once. Threading it
// through the run's own call chain — where a bare threadID used to travel — is
// what keeps two concurrent conversations from writing each other's versions.
//
// A session is always non-nil while a run is executing, even for an agent with
// no memory at all: thread is then empty and every method is a no-op, so the
// call sites stay free of nil checks and of "is memory on" branches.
type memorySession struct {
	agent  *aiAgent
	thread string
	ref    core.MemoryRef
	// store is the first-class store, and nil when this run is not using one —
	// either the agent declared no agentId, or the module has no store to offer.
	// Its absence is what selects the legacy KV transcript path.
	store core.AgentMemory
	// version is the working-memory version this run last wrote, carried so the
	// next write can detect that something else has written in between.
	version int64
	// legacy records that this run's working memory was read from the pre-store KV
	// blob, so the first successful store write can clean the old key up.
	legacy bool
	// opening is the user turn this run started from, held for the history record.
	opening string
	// recorded guards against writing the opening turn to history twice, which the
	// stop-then-finish paths could otherwise do.
	recorded bool
	// fresh says this run opened the conversation, which is the one moment the
	// engine has any business naming it. See recordTurn.
	fresh bool
	// blind says the stored working memory could not be read, so this run is
	// carrying on without knowing what was there. See loadWorking.
	blind bool

	// Checkpoint debounce state. lastAt and lastSize describe the last write.
	mu        sync.Mutex
	lastAt    time.Time
	lastSize  int
	iteration int
}

// lastIteration is how far into its loop the run had got when memory was last
// touched. It is stored beside working memory so an operator looking at a
// checkpoint can tell a run that stalled on its first turn from one that stalled
// on its twentieth.
func (s *memorySession) lastIteration() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.iteration
}

// noteIteration records how far the run has got.
func (s *memorySession) noteIteration(iter int) {
	s.mu.Lock()
	s.iteration = iter
	s.mu.Unlock()
}

// newMemorySession builds the session for a run. store is nil unless the agent
// named itself and the module actually has somewhere to put things.
func (a *aiAgent) newMemorySession(ctx context.Context, msg *types.Message, thread string) *memorySession {
	sess := &memorySession{agent: a, thread: thread}
	if a.agentID == "" || thread == "" {
		return sess
	}
	store := core.RuntimeServicesFromContext(ctx).AgentMemory()
	if !store.Enabled() {
		// A deployment with no store is a supported deployment: the agent falls back
		// to the per-thread KV transcript and keeps working, minus the durable record.
		// Said once per run at debug, because for standalone-in-memory it is normal.
		slog.Debug("ai-agent has an agentId but the runtime keeps no agent memory",
			"block", a.name, "agent", a.agentID)
		return sess
	}
	sess.store = store
	sess.ref = core.MemoryRef{AgentID: a.agentID, ThreadKey: thread, UserID: a.resolveUser(msg)}
	return sess
}

// resolveUser evaluates who the agent is talking to, or returns empty when the
// block names nobody. A failure is logged rather than raised: not knowing the
// user costs user memory, and it should not cost the conversation.
func (a *aiAgent) resolveUser(msg *types.Message) string {
	if a.userID == nil {
		return ""
	}
	user, err := a.userID.EvalString(expr.MessageActivation(msg, a.env))
	if err != nil {
		slog.Warn("ai-agent userId did not resolve; user memory is off for this run",
			"block", a.name, "error", err)
		return ""
	}
	return user
}

// active reports whether this run is using the first-class store.
func (s *memorySession) active() bool { return s != nil && s.store != nil }

// recording reports whether completed turns should reach durable history.
func (s *memorySession) recording() bool {
	return s.active() && s.agent.history == historyRecord
}

// loadWorking returns the transcript this run resumes from.
//
// It reads the store first and falls back to the pre-store KV blob when the
// store has nothing, so a conversation that was live before this existed carries
// on rather than restarting. That fallback is a read-side tolerance for data
// already written, not a second code path: the first save moves the thread over
// and deletes the old key. Delete it once no deployment can still be holding a
// pre-store transcript.
func (s *memorySession) loadWorking(ctx context.Context) (memoryEnvelope, error) {
	if !s.active() {
		return s.agent.loadHistory(ctx, s.thread)
	}
	wm, ok, err := s.store.LoadWorking(ctx, s.ref)
	if err != nil {
		// A store that cannot be read must not take the conversation with it: the run
		// carries on from nothing rather than failing.
		//
		// But it must not SAVE either. This run has no idea what was stored, so its
		// transcript is the current exchange alone — and writing that back would
		// replace a conversation somebody has been having with the last thing they
		// said, over a transient read failure. Blind is how the save path knows to
		// leave it alone.
		slog.Warn("ai-agent could not load working memory; continuing without it, and will not overwrite it",
			"block", s.agent.name, "agent", s.ref.AgentID, "thread", s.thread, "error", err)
		s.blind = true
		return memoryEnvelope{}, nil
	}
	if ok {
		s.version = wm.Version
		s.fresh = false
		env, decodeErr := decodeMemory(wm.Payload)
		if decodeErr != nil {
			slog.Warn("ai-agent stored working memory did not decode; continuing without it",
				"block", s.agent.name, "thread", s.thread, "error", decodeErr)
			return memoryEnvelope{}, nil
		}
		return env, nil
	}
	env, err := s.agent.loadHistory(ctx, s.thread)
	if err != nil {
		return memoryEnvelope{}, err
	}
	s.legacy = len(env.Messages) > 0
	s.fresh = !s.legacy
	return env, nil
}

// noteOpening records the user turn this run began with, so the history record
// can carry the question as well as the answer.
func (s *memorySession) noteOpening(text string) {
	if s != nil {
		s.opening = text
	}
}

// offerCheckpoint saves working memory mid-run when enough has changed to be
// worth a write. See checkpointInterval.
//
// It is deliberately not the same call as the terminal save: this one skips
// compaction (the run's own fitContext already bounds what is in flight) and
// tolerates every failure silently but for a debug line, because a checkpoint is
// an optimization for a run that may never be interrupted.
func (s *memorySession) offerCheckpoint(
	ctx context.Context, transcript []core.LLMMessage, meter *contextMeter, iter int,
) {
	if !s.active() {
		return
	}
	size := estimateTokens(transcript)
	s.mu.Lock()
	due := time.Since(s.lastAt) >= checkpointInterval || (size-s.lastSize)*charsPerToken >= checkpointBytes
	if !due {
		s.mu.Unlock()
		return
	}
	s.lastAt = time.Now()
	s.lastSize = size
	s.iteration = iter
	s.mu.Unlock()

	env := memoryEnvelope{Messages: transcript, Tokens: meter.sizeOfMessages(transcript)}
	if err := s.writeWorking(ctx, env, iter); err != nil {
		slog.Debug("ai-agent working-memory checkpoint did not land",
			"block", s.agent.name, "thread", s.thread, "error", err)
	}
}

// writeWorking stores the transcript and carries the new version forward. A
// version conflict re-reads and retries once: another replica of this agent
// wrote in between, and the latest state is the one worth keeping.
func (s *memorySession) writeWorking(ctx context.Context, env memoryEnvelope, iter int) error {
	if s.blind {
		// See loadWorking: this run never learned what was stored, so what it holds is
		// not the conversation — it is the tail of one.
		return nil
	}
	env.Version = memoryVersion
	payload, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("encode working memory: %w", err)
	}
	wm := core.WorkingMemory{
		Version:   s.version,
		Iteration: iter,
		Tokens:    env.Tokens,
		Payload:   payload,
	}
	next, err := s.store.SaveWorking(ctx, s.ref, wm)
	if errors.Is(err, core.ErrVersionConflict) {
		current, ok, loadErr := s.store.LoadWorking(ctx, s.ref)
		if loadErr != nil {
			return loadErr
		}
		if ok {
			wm.Version = current.Version
		} else {
			wm.Version = 0
		}
		next, err = s.store.SaveWorking(ctx, s.ref, wm)
	}
	if err != nil {
		return err
	}
	s.version = next
	s.dropLegacy(ctx)
	return nil
}

// dropLegacy removes the pre-store KV transcript once the thread has been
// carried into the store, so the two cannot diverge and the old key does not sit
// there forever. It runs once, and a failure is not worth reporting: the stale
// copy is unread from here on.
func (s *memorySession) dropLegacy(ctx context.Context) {
	if !s.legacy {
		return
	}
	s.legacy = false
	kv := core.RuntimeServicesFromContext(ctx).KV()
	if err := kv.Delete(ctx, s.agent.memoryNamespace, memoryKey(s.thread), 0); err != nil {
		slog.Debug("ai-agent could not remove the migrated legacy transcript",
			"block", s.agent.name, "thread", s.thread, "error", err)
	}
}

// recordTurn appends a completed exchange to durable conversation history.
//
// answered is empty for a run that ended without one — stopped, out of turns,
// refused. The question is still recorded in that case, marked unanswered:
// somebody asked it, and a history that shows only the exchanges that went well
// is not a history.
func (s *memorySession) recordTurn(
	ctx context.Context, msg *types.Message, answered string, iterations int, stopReason string,
) {
	if !s.recording() || s.recorded {
		return
	}
	s.recorded = true
	s.noteIteration(iterations)
	attrs, err := json.Marshal(turnAttrs{
		Iterations: iterations,
		Unanswered: answered == "",
		StopReason: stopReason,
	})
	if err != nil {
		attrs = nil
	}
	turns := make([]core.Turn, 0, 2)
	if s.opening != "" {
		turns = append(turns, core.Turn{Role: core.LLMRoleUser, Text: s.opening, Attrs: attrs})
	}
	if answered != "" {
		turns = append(turns, core.Turn{Role: core.LLMRoleAssistant, Text: answered, Attrs: attrs})
	}
	if len(turns) == 0 {
		return
	}
	if s.fresh {
		s.nameThread(ctx, msg, answered)
		s.fresh = false
	}
	if _, err := s.store.AppendTurns(ctx, s.ref, turns); err != nil {
		// History is the durable record, so a failure here is louder than a lost
		// checkpoint — but it still must not fail the run: the person got their answer,
		// and taking the flow down after the fact would not give them a better one.
		slog.Warn("ai-agent could not record the conversation turn",
			"block", s.agent.name, "agent", s.ref.AgentID, "thread", s.thread, "error", err)
	}
}

// nameThread names the conversation this run just opened.
//
// The name comes from the block's nameThread chain when it declares one, and
// from the opening question otherwise. Which is which matters: a chain is a model
// call, and what to ask it — which model, what prompt, how long a name — is the
// flow author's decision, not the runtime's. The fallback is a truncation, which
// is not a judgement about anything and is only there so a list has something to
// show.
//
// The WRITE is the engine's either way, and that is the whole point of the slot
// being a slot. The engine is holding the conversation's reference; the store
// knows how to record a title on whichever tier it is — standalone renames it on
// disk, the platform calls the orchestrator. A chain that had to do the writing
// would need the reference handed to it, would have to reassemble a key it never
// composed, and would be addressing the store the engine is already inside.
//
// Failure names nothing and says so quietly. A conversation without a title is
// listed by its key, which is worse to read and no worse to use — and taking the
// run down over a label, after the person already has their answer, would be a
// poor trade.
func (s *memorySession) nameThread(ctx context.Context, msg *types.Message, answered string) {
	title := titleFor(s.opening)
	if s.agent.namer != nil {
		named, err := s.askForName(ctx, msg, answered)
		if err != nil {
			// A chain that blew up has said nothing, so the fallback stands. This is
			// the one case where the truncation survives a declared chain.
			slog.Warn("ai-agent could not name the new conversation",
				"block", s.agent.name, "thread", s.thread, "error", err)
		} else {
			// Otherwise the chain's answer IS the title, empty included. An empty
			// answer is a decision — this exchange is not worth naming — and falling
			// back to the first line of the question would be overruling it.
			title = named
		}
	}
	if title == "" {
		// Nothing to record. A conversation with no title is listed by its key,
		// which is worse to read and no worse to use.
		return
	}
	if err := s.store.SetTitle(ctx, s.ref, title); err != nil {
		slog.Debug("ai-agent could not title the new conversation",
			"block", s.agent.name, "thread", s.thread, "error", err)
	}
}

// askForName runs the naming chain over the exchange and returns what it made of
// it, trimmed to the same ceiling a fallback title gets.
//
// The chain is given a message of its own rather than the run's: it is not part
// of the conversation, and a chain that set a variable on the live message would
// be writing into a transcript it was only asked to describe.
func (s *memorySession) askForName(
	ctx context.Context, msg *types.Message, answered string,
) (string, error) {
	// Correlated with the run and carrying its variables, exactly as the events
	// path builds its own message: a naming call that appeared in a trace with no
	// relation to the conversation it named would be the harder thing to read.
	named, err := types.NewMessage(correlationOf(msg))
	if err != nil {
		return "", err
	}
	if msg != nil {
		for name, v := range msg.Variables {
			named.Variables.Set(name, v)
		}
	}
	named.SetBody(map[string]any{
		"threadKey": s.thread,
		"agentId":   s.ref.AgentID,
		"userId":    s.ref.UserID,
		"question":  s.opening,
		"answer":    answered,
	})
	out, err := s.agent.namer.Process(ctx, named)
	if err != nil {
		return "", err
	}
	if out == nil {
		// The chain filtered its own message out, which is a way of declining.
		return "", nil
	}
	return clipRunes(strings.TrimSpace(nameFromBody(out.Body)), titleMaxLen), nil
}

// correlationOf is the run's correlation id, or empty when there is no message to
// take one from — which only happens in tests that drive a session directly.
func correlationOf(msg *types.Message) string {
	if msg == nil {
		return ""
	}
	return msg.CorrelationID
}

// nameFromBody reads a title out of whatever the chain answered with.
//
// Both shapes are expected, and which one you get is decided by the block the
// author reached for. ai-mapping always parses the model's reply as JSON, so a
// chain built on it answers with `{"title": "..."}`; a chain that ends in a
// plain transform or a set-payload answers with the string itself. Accepting
// both is what keeps the slot from dictating which block goes in it.
//
// An empty title, in either shape, is an answer rather than a failure: it says
// this exchange is not worth naming.
func nameFromBody(body any) string {
	switch v := body.(type) {
	case string:
		return v
	case map[string]any:
		for _, key := range []string{"title", "name"} {
			if s, ok := v[key].(string); ok {
				return s
			}
		}
	}
	return ""
}

// memoryPreamble is what the agent has been told to remember about this person,
// rendered as a single turn for the model to read.
//
// It is returned to be put in the request rather than in the transcript, and the
// difference is load-bearing: a preamble folded into the conversation would be
// persisted with it, so a memory corrected between runs would sit in working
// memory beside two stale copies of itself. In the request it is refreshed every
// run and never stored, which is the same relationship the system prompt has to
// the conversation.
func (s *memorySession) memoryPreamble(ctx context.Context) []core.LLMMessage {
	if !s.active() || !s.agent.userMemory || s.ref.UserID == "" {
		return nil
	}
	memories, err := s.store.Memories(ctx, s.ref)
	if err != nil {
		slog.Warn("ai-agent could not load user memory", "block", s.agent.name, "error", err)
		return nil
	}
	if len(memories) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("What you have previously chosen to remember about this person:\n")
	shown := 0
	for _, m := range memories {
		// Checked before the write and enforced during it. A single memory can be
		// longer than the whole budget — nothing bounds what an agent chooses to
		// remember — so a loop that only checked the running total would blow past the
		// cap by the size of one entry, which is exactly the entry most worth capping.
		if shown >= preambleMaxMemories || b.Len() >= preambleMaxChars {
			break
		}
		b.WriteString("- ")
		b.WriteString(m.Name)
		b.WriteString(": ")
		b.WriteString(clipRunes(m.Value, preambleMaxChars-b.Len()))
		b.WriteString("\n")
		shown++
	}
	if shown < len(memories) {
		// Said rather than silently dropped: the model is about to answer as if this
		// were everything it knows, and it has a tool for reaching the rest.
		fmt.Fprintf(&b, "\n(%d more are stored; use %s to look them up.)\n",
			len(memories)-shown, searchMemoryToolName)
	}
	b.WriteString("\nUse it when it is relevant. Do not repeat it back unprompted.")
	return []core.LLMMessage{{Role: core.LLMRoleUser, Text: b.String()}}
}

// titleFor mints a conversation's label from its first turn: the first line,
// trimmed to something that fits a list. A model-written title is better and is
// the platform's job — this is what a conversation is called until one arrives.
func titleFor(opening string) string {
	title := strings.TrimSpace(opening)
	if i := strings.IndexByte(title, '\n'); i >= 0 {
		title = title[:i]
	}
	if len(title) > titleMaxLen {
		// Back up to a rune start: cutting mid-rune would store an invalid UTF-8
		// sequence, and the first thing to encode it would turn the cut character
		// into a replacement glyph.
		cut := titleMaxLen
		for cut > 0 && !utf8.RuneStart(title[cut]) {
			cut--
		}
		title = strings.TrimSpace(title[:cut])
	}
	return title
}

// validateAgentMemoryConfig rejects a memory configuration that cannot mean what
// it says, before the block is built.
//
// The theme is that opting in has to be explicit both ways. history and
// userMemory both need somewhere durable to write, and the only thing that names
// it is agentId — so asking for either without one is a mistake worth naming
// rather than a setting to quietly ignore. The alternative, defaulting agentId
// from the block's address, was tried and reverted: see types.BlockConfig.AgentID.
func validateAgentMemoryConfig(cfg types.BlockConfig) error {
	if cfg.History != "" && cfg.History != historyRecord && cfg.History != historyOff {
		return fmt.Errorf("ai-agent history must be %q or %q, got %q",
			historyRecord, historyOff, cfg.History)
	}
	// Trimmed, because the builder trims: an agentId of "   " that passed validation
	// would reach configureAgentStore as empty, and the block would quietly lose the
	// history it had just been told to keep.
	if strings.TrimSpace(cfg.AgentID) == "" {
		switch {
		case cfg.History == historyRecord:
			return errors.New(
				"ai-agent history requires an agentId to record the conversation under")
		case cfg.UserMemory:
			return errors.New(
				"ai-agent userMemory requires an agentId to store the memories under")
		}
		return nil
	}
	if cfg.MemoryThreadID == "" {
		return errors.New(
			"ai-agent agentId requires a memoryThreadId: an agent stores its memory per " +
				"conversation, and that is what names one")
	}
	if cfg.UserMemory && strings.TrimSpace(cfg.UserID) == "" {
		return errors.New("ai-agent userMemory requires a userId to attribute memories to")
	}
	return nil
}

// configureAgentStore wires the first-class memory half of an ai-agent: who the
// agent is, who it is talking to, and what it records.
//
// The default for history is "record" only once an agentId exists. An agent
// without one is left exactly as it was before any of this — the per-thread KV
// transcript, nothing durable — which is what keeps every flow written against
// the older behaviour building and running unchanged.
func (b *builder) configureAgentStore(block *aiAgent, cfg types.BlockConfig) error {
	block.agentID = strings.TrimSpace(cfg.AgentID)
	block.userMemory = cfg.UserMemory
	block.history = cfg.History
	if block.history == "" {
		if block.agentID == "" || cfg.MemoryVolatile {
			// Volatile memory is for a conversation whose loss costs nothing — a
			// specialist working a thread its caller minted for it. Recording those to
			// durable history would defeat the tier's whole purpose, accumulating a
			// conversation row per delegation forever. An author who genuinely wants both
			// can still say so explicitly.
			block.history = historyOff
		} else {
			block.history = historyRecord
		}
	}
	if strings.TrimSpace(cfg.UserID) == "" {
		return nil
	}
	userID, err := expr.CompileMessage(b.deps.Resources, cfg.UserID)
	if err != nil {
		return fmt.Errorf("ai-agent userId: %w", err)
	}
	block.userID = userID
	return nil
}

// The built-in tools an agent with user memory gets. They are reserved names: a
// flow declaring a tool or skill of the same name fails to build, the same way
// load_skill is protected.
const (
	rememberToolName     = "remember"
	forgetToolName       = "forget"
	searchMemoryToolName = "search_memory"
)

// memoryToolNames is every name the memory tools claim, for the builder's
// collision check.
var memoryToolNames = []string{rememberToolName, forgetToolName, searchMemoryToolName}

// searchMemoryLimit caps what one search returns. It is a recall aid, not a
// dump: a model handed fifty half-relevant fragments reasons worse than one
// handed five good ones, and pays for the privilege.
const searchMemoryLimit = 10

// userMemoryTools describes the memory tools to the model.
//
// There is deliberately no "recall" tool. Recall is not a decision worth asking
// a model to make — what it already knows about a person is context it needs
// before it can tell whether it needs it, and an agent that has to remember to
// remember mostly does not. So what is stored arrives in the request every turn
// (see memoryPreamble), and the tools are only for the three things that really
// are decisions: keep this, drop that, go looking for something older.
func userMemoryTools() []core.LLMTool {
	return []core.LLMTool{
		{
			Name: rememberToolName,
			Description: "Remember a durable fact, preference or piece of context about the " +
				"person you are talking to, so it is available in later conversations. Use it for " +
				"things that stay true, not for what was just said. Re-using an existing name " +
				"replaces that memory.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` +
				`"name":{"type":"string","description":"Short stable handle, e.g. \"prefers-go-examples\"."},` +
				`"value":{"type":"string","description":"The fact, in one or two sentences."}},` +
				`"required":["name","value"]}`),
		},
		{
			Name:        forgetToolName,
			Description: "Forget a previously remembered fact about this person, by its name.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` +
				`"name":{"type":"string","description":"The name the memory was stored under."}},` +
				`"required":["name"]}`),
		},
		{
			Name: searchMemoryToolName,
			Description: "Search earlier conversations and remembered facts for something you no " +
				"longer have in context. Use it when the answer depends on something said before " +
				"this conversation.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` +
				`"query":{"type":"string","description":"What to look for, in natural language."}},` +
				`"required":["query"]}`),
		},
	}
}

// runMemoryTool serves one of the built-in memory tools. handled is false for
// any other name, so the caller falls through to the flow's own branches.
//
// Every failure comes back as a tool result rather than an error: a store that
// is briefly unavailable should cost the model a retry, not take down the
// conversation it is in the middle of.
func (a *aiAgent) runMemoryTool(
	ctx context.Context, call core.LLMToolCall, sess *memorySession,
) (core.LLMToolResult, bool) {
	// Only an agent that was actually handed these tools may claim their names. A
	// flow that declared its own "remember" branch and never asked for user memory
	// keeps it: the builder only rejects that collision when userMemory is on, so
	// intercepting here regardless would break a flow that builds fine.
	if !a.userMemory {
		return core.LLMToolResult{}, false
	}
	switch call.Name {
	case rememberToolName, forgetToolName, searchMemoryToolName:
	default:
		return core.LLMToolResult{}, false
	}
	if !sess.active() {
		return errorResult(call, "memory is not available to this agent"), true
	}
	var args struct {
		Name  string `json:"name"`
		Value string `json:"value"`
		Query string `json:"query"`
	}
	if err := json.Unmarshal(call.Input, &args); err != nil {
		return errorResult(call, "invalid arguments"), true
	}
	switch call.Name {
	case rememberToolName:
		return sess.remember(ctx, call, args.Name, args.Value), true
	case forgetToolName:
		return sess.forget(ctx, call, args.Name), true
	default:
		return sess.searchMemory(ctx, call, args.Query), true
	}
}

// remember stores or replaces one curated memory.
//
// It writes at version 0 and, on the conflict that means "this name already
// exists", re-reads and writes over it. That is the shape the model expects:
// telling an agent something it already believes should correct the belief, not
// fail.
func (s *memorySession) remember(
	ctx context.Context, call core.LLMToolCall, name, value string,
) core.LLMToolResult {
	name, value = strings.TrimSpace(name), strings.TrimSpace(value)
	if name == "" || value == "" {
		return errorResult(call, "name and value are both required")
	}
	if s.ref.UserID == "" {
		return errorResult(call, "there is no identified person to remember this about")
	}
	_, err := s.store.PutMemory(ctx, s.ref, name, value, 0)
	if errors.Is(err, core.ErrVersionConflict) {
		existing, findErr := s.findMemory(ctx, name)
		if findErr != nil {
			return errorResult(call, findErr.Error())
		}
		_, err = s.store.PutMemory(ctx, s.ref, name, value, existing)
	}
	if err != nil {
		slog.Warn("ai-agent could not store a user memory",
			"block", s.agent.name, "name", name, "error", err)
		return errorResult(call, "could not store that memory")
	}
	slog.Info("ai-agent remembered", "block", s.agent.name, "agent", s.ref.AgentID, "name", name)
	return core.LLMToolResult{ToolCallID: call.ID, Tool: call.Name, Content: "Remembered " + name + "."}
}

// findMemory returns the current version of a named memory.
func (s *memorySession) findMemory(ctx context.Context, name string) (int64, error) {
	memories, err := s.store.Memories(ctx, s.ref)
	if err != nil {
		return 0, errors.New("could not read existing memories")
	}
	for _, m := range memories {
		if m.Name == name {
			return m.Version, nil
		}
	}
	return 0, nil
}

// forget removes one curated memory. A name that was never stored is reported as
// done rather than as an error: the end state the model asked for is the end
// state it has.
func (s *memorySession) forget(ctx context.Context, call core.LLMToolCall, name string) core.LLMToolResult {
	name = strings.TrimSpace(name)
	if name == "" {
		return errorResult(call, "name is required")
	}
	if err := s.store.DeleteMemory(ctx, s.ref, name); err != nil {
		slog.Warn("ai-agent could not forget a user memory",
			"block", s.agent.name, "name", name, "error", err)
		return errorResult(call, "could not forget that memory")
	}
	slog.Info("ai-agent forgot", "block", s.agent.name, "agent", s.ref.AgentID, "name", name)
	return core.LLMToolResult{ToolCallID: call.ID, Tool: call.Name, Content: "Forgot " + name + "."}
}

// searchMemory looks through this agent's stored conversations and memories.
// Whether that ranks semantically or by text is the store's business — see
// core.MemoryCapabilities — and not something the model is told, because it
// would not change what it asked for.
func (s *memorySession) searchMemory(
	ctx context.Context, call core.LLMToolCall, query string,
) core.LLMToolResult {
	query = strings.TrimSpace(query)
	if query == "" {
		return errorResult(call, "query is required")
	}
	hits, err := s.store.Search(ctx, core.MemoryQuery{
		AgentID: s.ref.AgentID,
		UserID:  s.ref.UserID,
		Text:    query,
		Scope:   core.MemoryScopeAll,
		Limit:   searchMemoryLimit,
	})
	if err != nil {
		slog.Warn("ai-agent memory search failed", "block", s.agent.name, "error", err)
		return errorResult(call, "could not search memory")
	}
	if len(hits) == 0 {
		return core.LLMToolResult{
			ToolCallID: call.ID, Tool: call.Name,
			Content: "Nothing stored matches that.",
		}
	}
	var b strings.Builder
	for _, h := range hits {
		switch h.Kind {
		case core.MemoryHitUser:
			b.WriteString("remembered ")
			b.WriteString(h.Name)
			b.WriteString(": ")
		default:
			b.WriteString("from an earlier conversation: ")
		}
		b.WriteString(h.Text)
		b.WriteString("\n")
	}
	return core.LLMToolResult{ToolCallID: call.ID, Tool: call.Name, Content: b.String()}
}

// withPreamble puts the memory preamble in front of the conversation for one
// request, without touching the transcript the caller owns. A run with no
// preamble sends the transcript itself, allocating nothing.
func withPreamble(preamble, messages []core.LLMMessage) []core.LLMMessage {
	if len(preamble) == 0 {
		return messages
	}
	out := make([]core.LLMMessage, 0, len(preamble)+len(messages))
	out = append(out, preamble...)
	return append(out, messages...)
}

// rejectMemoryToolCollision fails a build where a flow's own tool or skill has
// taken one of the memory tools' names, so the model is never handed two tools
// called the same thing — one of which it cannot reach.
func rejectMemoryToolCollision(tools []core.LLMTool) error {
	taken := make(map[string]bool, len(tools))
	for _, t := range tools {
		taken[t.Name] = true
	}
	for _, name := range memoryToolNames {
		if taken[name] {
			return fmt.Errorf("ai-agent tool %q uses a name reserved by user memory", name)
		}
	}
	return nil
}

// clipRunes cuts text to at most n bytes on a rune boundary, marking the cut when
// one happens.
//
// Runes rather than bytes because the result is put in front of a model: an
// invalid sequence is not a smaller string, it is a string with a replacement
// character where a word was. A non-positive budget yields nothing.
func clipRunes(text string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(text) <= n {
		return text
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return strings.TrimSpace(text[:cut]) + "…"
}
