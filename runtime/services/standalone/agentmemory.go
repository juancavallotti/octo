package standalone

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/juancavallotti/octo/runtime/core"
)

// agentMemory is the standalone module's agent-memory store: a file tree under
// the storage directory.
//
// The layout is the addressing, not an index:
//
//	agent-memory/{agent}/threads/{thread}/thread.json     metadata + version
//	agent-memory/{agent}/threads/{thread}/working.json    live context + version
//	agent-memory/{agent}/threads/{thread}/turns.jsonl     one completed turn a line
//	agent-memory/{agent}/users/{user}.json                curated memories + version
//
// Listing a conversation is therefore a ReadDir, and there is no second file to
// keep in step with the first. That is worth stating because the thing this
// store replaces had to maintain an index by hand — an agent storing its own
// history in KV has get, set and delete and no way to ask what it has written,
// so it kept a list object beside the data and capped it to stop it growing.
// Neither problem exists once the store can enumerate.
//
// Concurrency is in-process only, and deliberately. The standalone module is one
// process — its leader election grants unconditionally because there is nothing
// to elect — so a per-thread mutex is the complete answer rather than a
// degraded one. Two standalone replicas over a shared volume is not a supported
// shape, and pretending otherwise would mean a multi-writer append protocol on
// a filesystem where O_APPEND is not even atomic.
type agentMemory struct {
	root string

	// mu guards locks; each thread has its own mutex so two conversations do not
	// serialize against each other. The same shape leases.go uses.
	mu    sync.Mutex
	locks map[string]*sync.Mutex

	// mem is the whole store when there is no storage directory — a one-shot
	// invocation that should leave nothing behind still wants coherent memory
	// while it runs. nil when writing to disk.
	mem *memoryOnlyStore
}

// File and directory names. threadsDir and usersDir keep the two kinds of object
// apart so a user id can never collide with a thread key.
const (
	memoryRootDir  = "agent-memory"
	threadsDir     = "threads"
	usersDir       = "users"
	threadFile     = "thread.json"
	workingFile    = "working.json"
	turnsFile      = "turns.jsonl"
	userMemoryFile = ".json"
)

// defaultPageLimit bounds a listing that asked for nothing in particular.
const defaultPageLimit = 50

// searchTurnWindow is how many of a thread's most recent turns the keyword
// search reads. Older turns are not unreachable — a caller can read the thread —
// but a search that walked every turn of every conversation would get slower for
// the life of the deployment, and the tail is where a question is almost always
// answered from.
const searchTurnWindow = 500

// newAgentMemory returns the module's store. An empty dir keeps everything in
// process memory, mirroring newStore.
func newAgentMemory(dir string) *agentMemory {
	m := &agentMemory{locks: map[string]*sync.Mutex{}}
	if dir == "" {
		m.mem = newMemoryOnlyStore()
		return m
	}
	m.root = filepath.Join(dir, memoryRootDir)
	return m
}

func (m *agentMemory) Enabled() bool { return true }

// Capabilities reports no semantic search. Embedding needs a provider and a
// credential, which are platform configuration the standalone module has no
// access to — and reaching for one from here would make services import a
// connector, inverting the two extension points. Search still works; it matches
// text.
func (m *agentMemory) Capabilities() core.MemoryCapabilities {
	return core.MemoryCapabilities{Semantic: false}
}

// lockFor returns the mutex guarding one path, creating it on first use.
func (m *agentMemory) lockFor(key string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	lock, ok := m.locks[key]
	if !ok {
		lock = &sync.Mutex{}
		m.locks[key] = lock
	}
	return lock
}

// threadDir is where one conversation's files live.
func (m *agentMemory) threadDir(ref core.MemoryRef) string {
	return filepath.Join(m.root, encodeName(ref.AgentID), threadsDir, encodeName(ref.ThreadKey))
}

// userFile is where one person's curated memories live.
func (m *agentMemory) userFile(ref core.MemoryRef) string {
	return filepath.Join(m.root, encodeName(ref.AgentID), usersDir, encodeName(ref.UserID)+userMemoryFile)
}

// storedThread is thread.json: the metadata a listing shows without opening the
// conversation.
type storedThread struct {
	AgentID        string    `json:"agentId"`
	ThreadKey      string    `json:"threadKey"`
	UserID         string    `json:"userId,omitempty"`
	Title          string    `json:"title,omitempty"`
	Version        int64     `json:"version"`
	TurnCount      int       `json:"turnCount"`
	CreatedAt      time.Time `json:"createdAt"`
	LastActivityAt time.Time `json:"lastActivityAt"`
}

func (t storedThread) toCore() core.Thread {
	return core.Thread{
		AgentID: t.AgentID, ThreadKey: t.ThreadKey, UserID: t.UserID, Title: t.Title,
		Version: t.Version, TurnCount: t.TurnCount,
		CreatedAt: t.CreatedAt, LastActivityAt: t.LastActivityAt,
	}
}

// storedWorking is working.json.
type storedWorking struct {
	Version   int64     `json:"version"`
	Iteration int       `json:"iteration"`
	Tokens    int       `json:"tokens"`
	Payload   []byte    `json:"payload"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// storedTurn is one line of turns.jsonl.
type storedTurn struct {
	Seq       int64           `json:"seq"`
	Role      core.LLMRole    `json:"role"`
	Text      string          `json:"text"`
	Tokens    int             `json:"tokens,omitempty"`
	Attrs     json.RawMessage `json:"attrs,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
}

// storedUserMemories is one person's file: every memory in one object, because
// they are always read together.
type storedUserMemories struct {
	Items map[string]storedUserMemory `json:"items"`
}

type storedUserMemory struct {
	Value     string    `json:"value"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// LoadWorking reads a conversation's live context.
func (m *agentMemory) LoadWorking(_ context.Context, ref core.MemoryRef) (core.WorkingMemory, bool, error) {
	if m.mem != nil {
		return m.mem.loadWorking(ref)
	}
	var w storedWorking
	ok, err := readJSONFile(filepath.Join(m.threadDir(ref), workingFile), &w)
	if err != nil || !ok {
		return core.WorkingMemory{}, false, err
	}
	return core.WorkingMemory{
		Version: w.Version, Iteration: w.Iteration, Tokens: w.Tokens,
		Payload: w.Payload, UpdatedAt: w.UpdatedAt,
	}, true, nil
}

// SaveWorking stores the live context, creating the conversation if it is new.
//
// The version is checked under the thread's own lock and the file is replaced
// atomically, so a reader never sees a half-written context and two writers
// never lose one of their updates silently.
func (m *agentMemory) SaveWorking(
	_ context.Context, ref core.MemoryRef, wm core.WorkingMemory,
) (int64, error) {
	if m.mem != nil {
		return m.mem.saveWorking(ref, wm)
	}
	dir := m.threadDir(ref)
	lock := m.lockFor(dir)
	lock.Lock()
	defer lock.Unlock()

	var current storedWorking
	if _, err := readJSONFile(filepath.Join(dir, workingFile), &current); err != nil {
		return 0, err
	}
	if wm.Version != current.Version {
		return 0, core.ErrVersionConflict
	}
	next := storedWorking{
		Version:   current.Version + 1,
		Iteration: wm.Iteration,
		Tokens:    wm.Tokens,
		Payload:   wm.Payload,
		UpdatedAt: time.Now().UTC(),
	}
	if err := m.ensureThread(dir, ref, next.UpdatedAt); err != nil {
		return 0, err
	}
	if err := writeJSONFile(filepath.Join(dir, workingFile), next); err != nil {
		return 0, err
	}
	return next.Version, nil
}

// ensureThread creates the conversation's metadata file if this is the first
// write to it, and stamps its activity time. The caller holds the thread's lock.
func (m *agentMemory) ensureThread(dir string, ref core.MemoryRef, at time.Time) error {
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	path := filepath.Join(dir, threadFile)
	var t storedThread
	found, err := readJSONFile(path, &t)
	if err != nil {
		return err
	}
	if !found {
		t = storedThread{
			AgentID: ref.AgentID, ThreadKey: ref.ThreadKey, UserID: ref.UserID,
			CreatedAt: at,
		}
	}
	if t.UserID == "" {
		t.UserID = ref.UserID
	}
	t.Version++
	t.LastActivityAt = at
	return writeJSONFile(path, t)
}

// AppendTurns adds completed turns to the durable record.
//
// Appended under the thread's lock, with the sequence numbers taken from the
// metadata file, so two runs on one conversation interleave rather than
// overwrite. This is the one file opened for append rather than replaced: a
// transcript is the thing this store exists to not lose, and rewriting it whole
// on every turn would put the entire conversation at risk of every write.
func (m *agentMemory) AppendTurns(
	_ context.Context, ref core.MemoryRef, turns []core.Turn,
) (int64, error) {
	if m.mem != nil {
		return m.mem.appendTurns(ref, turns)
	}
	if len(turns) == 0 {
		return 0, nil
	}
	dir := m.threadDir(ref)
	lock := m.lockFor(dir)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now().UTC()
	if err := m.ensureThread(dir, ref, now); err != nil {
		return 0, err
	}
	var t storedThread
	if _, err := readJSONFile(filepath.Join(dir, threadFile), &t); err != nil {
		return 0, err
	}

	//nolint:gosec // the path is one this package composed and wrote
	f, err := os.OpenFile(filepath.Join(dir, turnsFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePerm)
	if err != nil {
		return 0, fmt.Errorf("opening %s: %w", turnsFile, err)
	}
	defer func() { _ = f.Close() }()

	var b strings.Builder
	for _, turn := range turns {
		t.TurnCount++
		line, marshalErr := json.Marshal(storedTurn{
			Seq: int64(t.TurnCount), Role: turn.Role, Text: turn.Text,
			Tokens: turn.Tokens, Attrs: turn.Attrs, CreatedAt: now,
		})
		if marshalErr != nil {
			return 0, fmt.Errorf("encoding a turn: %w", marshalErr)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	if _, err := f.WriteString(b.String()); err != nil {
		return 0, fmt.Errorf("appending to %s: %w", turnsFile, err)
	}
	if err := f.Sync(); err != nil {
		return 0, fmt.Errorf("flushing %s: %w", turnsFile, err)
	}
	t.LastActivityAt = now
	if err := writeJSONFile(filepath.Join(dir, threadFile), t); err != nil {
		return 0, err
	}
	return t.Version, nil
}

// ListThreads enumerates an agent's conversations, most recently active first.
//
// The directory is the index: there is no list object to keep in step, which is
// the whole reason this is a store rather than a convention over KV.
func (m *agentMemory) ListThreads(
	_ context.Context, agentID, userID string, page core.Page,
) ([]core.Thread, string, error) {
	if m.mem != nil {
		return m.mem.listThreads(agentID, userID, page)
	}
	rows, err := m.allThreads(agentID, userID)
	if err != nil {
		return nil, "", err
	}
	return pageThreads(rows, page)
}

// allThreads reads every one of an agent's conversations, in listing order.
//
// It is separate from ListThreads because a search has to see all of them, and
// asking the paged listing for "everything" has no honest spelling — a limit of
// zero or below means "use the default", so a caller trying to say unlimited
// silently gets the first page and reports its findings as if they were the
// whole store.
func (m *agentMemory) allThreads(agentID, userID string) ([]core.Thread, error) {
	dir := filepath.Join(m.root, encodeName(agentID), threadsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	rows := make([]core.Thread, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var t storedThread
		ok, readErr := readJSONFile(filepath.Join(dir, e.Name(), threadFile), &t)
		if readErr != nil || !ok {
			// A thread directory with no readable metadata is skipped rather than
			// failing the listing, for the same reason a corrupt namespace file does not
			// stop the store loading: one damaged conversation must not hide the rest.
			continue
		}
		if userID != "" && t.UserID != userID {
			continue
		}
		rows = append(rows, t.toCore())
	}
	sortThreads(rows)
	return rows, nil
}

// sortThreads orders a listing: most recently active first, thread key breaking
// ties so the order is total and a cursor cannot loop.
func sortThreads(rows []core.Thread) {
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].LastActivityAt.Equal(rows[j].LastActivityAt) {
			return rows[i].LastActivityAt.After(rows[j].LastActivityAt)
		}
		return rows[i].ThreadKey < rows[j].ThreadKey
	})
}

// pageThreads applies the cursor and limit to an ordered listing. The cursor is
// the last thread key returned, which is unambiguous because the order is total.
func pageThreads(rows []core.Thread, page core.Page) ([]core.Thread, string, error) {
	limit := page.Limit
	if limit <= 0 {
		limit = defaultPageLimit
	}
	start, found := 0, false
	if page.Cursor != "" {
		after, err := base64.RawURLEncoding.DecodeString(page.Cursor)
		if err != nil {
			return nil, "", fmt.Errorf("bad cursor: %w", err)
		}
		for i := range rows {
			if rows[i].ThreadKey == string(after) {
				start, found = i+1, true
				break
			}
		}
		if !found {
			// The conversation the cursor named is gone — deleted between two pages, or
			// from a listing that no longer exists. Treated as exhausted rather than as
			// "start again", because starting again is what turns a page-until-empty
			// loop into a loop that never ends.
			return nil, "", nil
		}
	}
	if start >= len(rows) {
		return nil, "", nil
	}
	end := min(start+limit, len(rows))
	next := ""
	if end < len(rows) {
		next = base64.RawURLEncoding.EncodeToString([]byte(rows[end-1].ThreadKey))
	}
	return rows[start:end], next, nil
}

// ReadThread returns a conversation's metadata and its turns in order.
func (m *agentMemory) ReadThread(
	_ context.Context, ref core.MemoryRef, page core.Page,
) (core.Thread, []core.Turn, string, error) {
	if m.mem != nil {
		return m.mem.readThread(ref, page)
	}
	dir := m.threadDir(ref)
	var t storedThread
	ok, err := readJSONFile(filepath.Join(dir, threadFile), &t)
	if err != nil || !ok {
		return core.Thread{}, nil, "", err
	}
	turns, err := readTurns(filepath.Join(dir, turnsFile), 0)
	if err != nil {
		return core.Thread{}, nil, "", err
	}
	turns, next := pageTurns(turns, page)
	return t.toCore(), turns, next, nil
}

// pageTurns applies the cursor and limit to a transcript. The cursor is the last
// sequence number returned, which orders totally by construction.
func pageTurns(turns []core.Turn, page core.Page) ([]core.Turn, string) {
	limit := page.Limit
	if limit <= 0 {
		limit = defaultPageLimit
	}
	start, found := 0, false
	if page.Cursor != "" {
		after, err := base64.RawURLEncoding.DecodeString(page.Cursor)
		if err != nil {
			return nil, ""
		}
		for i := range turns {
			if fmt.Sprint(turns[i].Seq) == string(after) {
				start, found = i+1, true
				break
			}
		}
		if !found {
			// Same decision as pageThreads: a cursor naming a turn that is no longer
			// there is exhausted, not a restart. A truncated transcript would otherwise
			// replay itself from turn one forever.
			return nil, ""
		}
	}
	if start >= len(turns) {
		return nil, ""
	}
	end := min(start+limit, len(turns))
	next := ""
	if end < len(turns) {
		next = base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprint(turns[end-1].Seq)))
	}
	return turns[start:end], next
}

// DeleteThread removes a conversation entirely.
//
// Everything, and in one call: this is what "forget this conversation" reaches,
// and a deletion that left the transcript behind while removing the metadata
// would report success over a readable copy.
func (m *agentMemory) DeleteThread(_ context.Context, ref core.MemoryRef) error {
	if m.mem != nil {
		return m.mem.deleteThread(ref)
	}
	dir := m.threadDir(ref)
	lock := m.lockFor(dir)
	lock.Lock()
	defer lock.Unlock()
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("removing %s: %w", dir, err)
	}
	return nil
}

// SetTitle names a conversation.
func (m *agentMemory) SetTitle(_ context.Context, ref core.MemoryRef, title string) error {
	if m.mem != nil {
		return m.mem.setTitle(ref, title)
	}
	dir := m.threadDir(ref)
	lock := m.lockFor(dir)
	lock.Lock()
	defer lock.Unlock()
	now := time.Now().UTC()
	if err := m.ensureThread(dir, ref, now); err != nil {
		return err
	}
	path := filepath.Join(dir, threadFile)
	var t storedThread
	if _, err := readJSONFile(path, &t); err != nil {
		return err
	}
	t.Title = title
	return writeJSONFile(path, t)
}

// Memories returns everything the agent has kept about one person.
func (m *agentMemory) Memories(_ context.Context, ref core.MemoryRef) ([]core.UserMemory, error) {
	if m.mem != nil {
		return m.mem.memories(ref)
	}
	stored, _, err := readUserMemories(m.userFile(ref))
	if err != nil {
		return nil, err
	}
	return sortedMemories(stored), nil
}

// sortedMemories flattens a person's file into a stable order, so a preamble
// built from it does not shuffle between runs for no reason.
func sortedMemories(stored storedUserMemories) []core.UserMemory {
	out := make([]core.UserMemory, 0, len(stored.Items))
	for name, item := range stored.Items {
		out = append(out, core.UserMemory{
			Name: name, Value: item.Value, Version: item.Version,
			CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// PutMemory creates or updates one curated memory.
func (m *agentMemory) PutMemory(
	_ context.Context, ref core.MemoryRef, name, value string, expectedVersion int64,
) (int64, error) {
	if m.mem != nil {
		return m.mem.putMemory(ref, name, value, expectedVersion)
	}
	path := m.userFile(ref)
	lock := m.lockFor(path)
	lock.Lock()
	defer lock.Unlock()

	stored, _, err := readUserMemories(path)
	if err != nil {
		return 0, err
	}
	current := stored.Items[name]
	if current.Version != expectedVersion {
		return 0, core.ErrVersionConflict
	}
	now := time.Now().UTC()
	if current.CreatedAt.IsZero() {
		current.CreatedAt = now
	}
	current.Value = value
	current.Version++
	current.UpdatedAt = now
	stored.Items[name] = current
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return 0, fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := writeJSONFile(path, stored); err != nil {
		return 0, err
	}
	return current.Version, nil
}

// DeleteMemory forgets one curated memory. A name that was never stored is not
// an error: the end state the caller asked for is the end state they have.
func (m *agentMemory) DeleteMemory(_ context.Context, ref core.MemoryRef, name string) error {
	if m.mem != nil {
		return m.mem.deleteMemory(ref, name)
	}
	path := m.userFile(ref)
	lock := m.lockFor(path)
	lock.Lock()
	defer lock.Unlock()

	stored, found, err := readUserMemories(path)
	if err != nil || !found {
		return err
	}
	if _, ok := stored.Items[name]; !ok {
		return nil
	}
	delete(stored.Items, name)
	return writeJSONFile(path, stored)
}

// Search matches text across an agent's conversations and stored memories.
//
// Keyword, because this module has no embeddings (see Capabilities). The scoring
// is deliberately simple — how many of the query's words a candidate contains,
// damped by its length so a long turn does not win by containing everything —
// and it is the fallback every store owes rather than an attempt at relevance.
func (m *agentMemory) Search(_ context.Context, q core.MemoryQuery) ([]core.MemoryHit, error) {
	if m.mem != nil {
		return m.mem.search(q)
	}
	words := queryWords(q.Text)
	if len(words) == 0 {
		return nil, nil
	}
	var hits []core.MemoryHit
	if q.Scope != core.MemoryScopeTurns && q.UserID != "" {
		stored, _, err := readUserMemories(m.userFile(core.MemoryRef{AgentID: q.AgentID, UserID: q.UserID}))
		if err != nil {
			return nil, err
		}
		for _, mem := range sortedMemories(stored) {
			if score := scoreText(mem.Value+" "+mem.Name, words); score > 0 {
				hits = append(hits, core.MemoryHit{
					Kind: core.MemoryHitUser, Name: mem.Name, Text: mem.Value, Score: score,
				})
			}
		}
	}
	if q.Scope != core.MemoryScopeUser {
		turnHits, err := m.searchTurns(q, words)
		if err != nil {
			return nil, err
		}
		hits = append(hits, turnHits...)
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	limit := q.Limit
	if limit <= 0 || limit > len(hits) {
		limit = len(hits)
	}
	return hits[:limit], nil
}

// searchTurns scans the recent tail of each of the agent's conversations.
func (m *agentMemory) searchTurns(
	q core.MemoryQuery, words []string,
) ([]core.MemoryHit, error) {
	threads, err := m.allThreads(q.AgentID, q.UserID)
	if err != nil {
		return nil, err
	}
	var hits []core.MemoryHit
	for _, t := range threads {
		if q.ThreadKey != "" && t.ThreadKey != q.ThreadKey {
			continue
		}
		ref := core.MemoryRef{AgentID: q.AgentID, ThreadKey: t.ThreadKey}
		turns, readErr := readTurns(filepath.Join(m.threadDir(ref), turnsFile), searchTurnWindow)
		if readErr != nil {
			continue
		}
		for _, turn := range turns {
			if score := scoreText(turn.Text, words); score > 0 {
				hits = append(hits, core.MemoryHit{
					Kind: core.MemoryHitTurn, ThreadKey: t.ThreadKey,
					Text: turn.Text, Seq: turn.Seq, Score: score,
				})
			}
		}
	}
	return hits, nil
}

// queryWords splits a query into the lowercase words a candidate is scored on.
func queryWords(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := fields[:0]
	for _, f := range fields {
		// Runes, not bytes: a two-character word in a non-Latin script is several
		// bytes long, and a byte-length floor would keep words it should drop while
		// an ASCII-only split predicate dropped the whole query.
		if len([]rune(f)) > 1 {
			out = append(out, f)
		}
	}
	return out
}

// lengthDamping is how quickly a candidate's length discounts its score: at this
// many characters a match counts for half what the same match counts for in a
// short one. It exists because a long turn is likelier to contain any given word,
// so it has to contain more of them to rank as high as a short one that is about
// exactly this.
const lengthDamping = 1000.0

// scoreText is the share of the query's words a candidate contains, damped by
// how much text it took to contain them.
func scoreText(text string, words []string) float64 {
	lower := strings.ToLower(text)
	matched := 0
	for _, w := range words {
		if strings.Contains(lower, w) {
			matched++
		}
	}
	if matched == 0 {
		return 0
	}
	share := float64(matched) / float64(len(words))
	damp := 1.0 + float64(len(text))/lengthDamping
	return share / damp
}

// readTurns reads a transcript file, optionally keeping only the last n turns.
//
// A trailing line that does not parse is dropped with a warning rather than
// failing the read: an interrupted append leaves exactly that, and one truncated
// turn must not make the conversation unreadable. It is the same tolerance the
// snapshot loader gives a corrupt namespace file, for the same reason.
func readTurns(path string, tail int) ([]core.Turn, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the path is one this package wrote
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if tail > 0 && len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	turns := make([]core.Turn, 0, len(lines))
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var st storedTurn
		if err := json.Unmarshal([]byte(line), &st); err != nil {
			slog.Warn("standalone: skipping an unreadable conversation turn",
				"file", path, "line", i+1, "error", err)
			continue
		}
		turns = append(turns, core.Turn{
			Seq: st.Seq, Role: st.Role, Text: st.Text,
			Tokens: st.Tokens, Attrs: st.Attrs, CreatedAt: st.CreatedAt,
		})
	}
	return turns, nil
}

// readUserMemories reads a person's file, returning an empty (usable) value when
// there is none yet.
func readUserMemories(path string) (storedUserMemories, bool, error) {
	stored := storedUserMemories{Items: map[string]storedUserMemory{}}
	found, err := readJSONFile(path, &stored)
	if err != nil {
		return storedUserMemories{Items: map[string]storedUserMemory{}}, false, err
	}
	if stored.Items == nil {
		stored.Items = map[string]storedUserMemory{}
	}
	return stored, found, nil
}

// readJSONFile decodes one JSON object, reporting whether the file existed. A
// file that exists but does not decode is an error: unlike a transcript line,
// there is no partial answer worth returning for a versioned object, and reading
// a corrupt one as "version 0" would overwrite whatever was really there.
func readJSONFile(path string, into any) (bool, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the path is one this package wrote
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return false, fmt.Errorf("decoding %s: %w", path, err)
	}
	return true, nil
}

// writeJSONFile replaces a file atomically: write a temporary, flush it, rename
// it into place, then flush the directory.
//
// The last step is the one that is easy to leave out and matters most. Sync on
// the file persists its contents; the rename is a change to the *directory*, and
// until that is flushed a power loss can leave the entry pointing at the old
// file — which for a versioned object means a version that has rewound under a
// caller who already advanced it. persist.go makes the same bargain and explains
// it at length.
func writeJSONFile(path string, value any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("creating a temporary file in %s: %w", dir, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if err := writeAndSync(tmp, encoded); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmp.Name(), err)
	}
	if err := os.Chmod(tmp.Name(), filePerm); err != nil {
		return fmt.Errorf("setting permissions on %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return syncDirPath(dir)
}

// writeAndSync writes the whole payload and flushes it to the device.
func writeAndSync(f *os.File, data []byte) error {
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("writing %s: %w", f.Name(), err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("flushing %s: %w", f.Name(), err)
	}
	return nil
}

// syncDirPath flushes a directory, tolerating filesystems that will not. See
// snapshot.syncDir, whose reasoning this shares.
func syncDirPath(dir string) error {
	d, err := os.Open(dir) //nolint:gosec // the directory is one this package composed
	if err != nil {
		return fmt.Errorf("opening %s to flush it: %w", dir, err)
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil && !unsupportedDirSync(err) {
		return fmt.Errorf("flushing %s: %w", dir, err)
	}
	return nil
}
