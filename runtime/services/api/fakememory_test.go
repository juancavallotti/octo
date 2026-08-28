package api

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// memoryBackend is the fake's agent-memory store: working memory, an append-only
// turn log, and curated memories, all versioned the way the contract requires.
type memoryBackend struct {
	mu       sync.Mutex
	working  map[string]*fakeWorking
	threads  map[string]*fakeThread
	memories map[string]*memoryWire
	now      time.Time
}

type fakeWorking struct {
	payload   []byte
	version   int64
	iteration int
	tokens    int
}

type fakeThread struct {
	meta  threadWire
	turns []turnWire
}

func newMemoryBackend() *memoryBackend {
	return &memoryBackend{
		working:  map[string]*fakeWorking{},
		threads:  map[string]*fakeThread{},
		memories: map[string]*memoryWire{},
		now:      time.Now(),
	}
}

func (b *memoryBackend) install(f *fake) {
	f.mux.HandleFunc("GET "+pathMemoryWorking, b.loadWorking)
	f.mux.HandleFunc("PUT "+pathMemoryWorking, b.saveWorking)
	f.mux.HandleFunc("POST "+pathMemoryThread+"/turns", b.appendTurns)
	f.mux.HandleFunc("GET /v1/agent-memory/{agentId}/threads", b.listThreads)
	f.mux.HandleFunc("GET "+pathMemoryThread, b.readThread)
	f.mux.HandleFunc("DELETE "+pathMemoryThread, b.deleteThread)
	f.mux.HandleFunc("PUT "+pathMemoryThread+"/title", b.setTitle)
	f.mux.HandleFunc("GET "+pathMemoryMemories, b.listMemories)
	f.mux.HandleFunc("PUT "+pathMemoryMemories, b.putMemory)
	f.mux.HandleFunc("DELETE "+pathMemoryMemories, b.deleteMemory)
	f.mux.HandleFunc("POST /v1/agent-memory/{agentId}/search", b.search)
}

// threadKey is how the fake scopes a conversation: by agent and thread only,
// never by user — a user segment would give one conversation two addresses.
func threadKey(r *http.Request) string {
	return r.PathValue("agentId") + "\x00" + r.PathValue("threadKey")
}

func (b *memoryBackend) loadWorking(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	wm, ok := b.working[threadKey(r)]
	b.mu.Unlock()
	if !ok {
		http.Error(w, "no working memory", http.StatusNotFound)
		return
	}
	w.Header().Set(headerVersion, strconv.FormatInt(wm.version, 10))
	w.Header().Set(headerIteration, strconv.Itoa(wm.iteration))
	w.Header().Set(headerTokens, strconv.Itoa(wm.tokens))
	_, _ = w.Write(wm.payload)
}

func (b *memoryBackend) saveWorking(w http.ResponseWriter, r *http.Request) {
	expected := parseVersion(r.Header.Get(headerVersion))
	payload, _ := io.ReadAll(r.Body)

	b.mu.Lock()
	defer b.mu.Unlock()
	key := threadKey(r)
	wm, exists := b.working[key]
	if (expected == 0 && exists) || (expected != 0 && (!exists || wm.version != expected)) {
		http.Error(w, "version conflict", http.StatusConflict)
		return
	}
	if !exists {
		wm = &fakeWorking{}
		b.working[key] = wm
		b.ensureThread(r)
	}
	wm.version++
	wm.payload = payload
	wm.iteration, _ = strconv.Atoi(r.Header.Get(headerIteration))
	wm.tokens, _ = strconv.Atoi(r.Header.Get(headerTokens))
	w.Header().Set(headerVersion, strconv.FormatInt(wm.version, 10))
	w.WriteHeader(http.StatusOK)
}

// ensureThread records the conversation, attributing it to the user named on the
// first write that names one — which is why omitting userId orphans a history.
func (b *memoryBackend) ensureThread(r *http.Request) *fakeThread {
	key := threadKey(r)
	th, ok := b.threads[key]
	if !ok {
		th = &fakeThread{meta: threadWire{
			AgentID: r.PathValue("agentId"), ThreadKey: r.PathValue("threadKey"),
			CreatedAt: b.now,
		}}
		b.threads[key] = th
	}
	if th.meta.UserID == "" {
		th.meta.UserID = r.URL.Query().Get("userId")
	}
	th.meta.LastActivityAt = b.now
	return th
}

func (b *memoryBackend) appendTurns(w http.ResponseWriter, r *http.Request) {
	var in appendRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	th := b.ensureThread(r)
	for _, t := range in.Turns {
		// Seq is assigned by the store, so two writers interleave rather than collide.
		t.Seq = int64(len(th.turns) + 1)
		th.turns = append(th.turns, t)
	}
	th.meta.Version++
	th.meta.TurnCount = len(th.turns)
	writeJSON(w, versionResponse{Version: th.meta.Version})
}

func (b *memoryBackend) listThreads(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentId")
	userID := r.URL.Query().Get("userId")

	b.mu.Lock()
	defer b.mu.Unlock()
	out := listThreadsResponse{}
	for _, th := range b.threads {
		if th.meta.AgentID != agentID || (userID != "" && th.meta.UserID != userID) {
			continue
		}
		out.Threads = append(out.Threads, th.meta)
	}
	sort.Slice(out.Threads, func(i, j int) bool {
		return out.Threads[i].ThreadKey < out.Threads[j].ThreadKey
	})
	writeJSON(w, out)
}

func (b *memoryBackend) readThread(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	th, ok := b.threads[threadKey(r)]
	if !ok {
		http.Error(w, "no such thread", http.StatusNotFound)
		return
	}
	writeJSON(w, readThreadResponse{Thread: th.meta, Turns: th.turns})
}

func (b *memoryBackend) deleteThread(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := threadKey(r)
	if _, ok := b.threads[key]; !ok {
		http.Error(w, "no such thread", http.StatusNotFound)
		return
	}
	delete(b.threads, key)
	delete(b.working, key)
	w.WriteHeader(http.StatusNoContent)
}

func (b *memoryBackend) setTitle(w http.ResponseWriter, r *http.Request) {
	var in titleRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ensureThread(r).meta.Title = in.Title
	w.WriteHeader(http.StatusNoContent)
}

func (b *memoryBackend) memoryKey(r *http.Request) string {
	return r.PathValue("agentId") + "\x00" + r.PathValue("userId") + "\x00" +
		r.URL.Query().Get("name")
}

func (b *memoryBackend) listMemories(w http.ResponseWriter, r *http.Request) {
	prefix := r.PathValue("agentId") + "\x00" + r.PathValue("userId") + "\x00"

	b.mu.Lock()
	defer b.mu.Unlock()
	out := memoriesResponse{}
	for key, mem := range b.memories {
		if strings.HasPrefix(key, prefix) {
			out.Memories = append(out.Memories, *mem)
		}
	}
	sort.Slice(out.Memories, func(i, j int) bool { return out.Memories[i].Name < out.Memories[j].Name })
	writeJSON(w, out)
}

func (b *memoryBackend) putMemory(w http.ResponseWriter, r *http.Request) {
	var in putMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	expected := parseVersion(r.Header.Get(headerVersion))

	b.mu.Lock()
	defer b.mu.Unlock()
	key := b.memoryKey(r)
	mem, exists := b.memories[key]
	if (expected == 0 && exists) || (expected != 0 && (!exists || mem.Version != expected)) {
		http.Error(w, "version conflict", http.StatusConflict)
		return
	}
	if !exists {
		mem = &memoryWire{Name: r.URL.Query().Get("name"), CreatedAt: b.now}
		b.memories[key] = mem
	}
	mem.Version++
	mem.Value = in.Value
	mem.UpdatedAt = b.now
	w.Header().Set(headerVersion, strconv.FormatInt(mem.Version, 10))
	w.WriteHeader(http.StatusOK)
}

func (b *memoryBackend) deleteMemory(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := b.memoryKey(r)
	if _, ok := b.memories[key]; !ok {
		http.Error(w, "no such memory", http.StatusNotFound)
		return
	}
	delete(b.memories, key)
	w.WriteHeader(http.StatusNoContent)
}

// search does a plain substring match, which is what a platform without
// embeddings does and what Capabilities().Semantic == false describes.
func (b *memoryBackend) search(w http.ResponseWriter, r *http.Request) {
	var in searchRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	agentID := r.PathValue("agentId")

	b.mu.Lock()
	defer b.mu.Unlock()
	out := searchResponse{}
	for _, th := range b.threads {
		if th.meta.AgentID != agentID {
			continue
		}
		for _, t := range th.turns {
			if strings.Contains(t.Text, in.Text) {
				out.Hits = append(out.Hits, hitWire{
					Kind: "turn", ThreadKey: th.meta.ThreadKey, Text: t.Text, Seq: t.Seq,
				})
			}
		}
	}
	writeJSON(w, out)
}

// userOf reports which user a conversation is attributed to.
func (b *memoryBackend) userOf(agentID, thread string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if th, ok := b.threads[agentID+"\x00"+thread]; ok {
		return th.meta.UserID
	}
	return ""
}
