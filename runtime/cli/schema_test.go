package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/schema"
)

// migratedBlocks and migratedConnectors are the entries carrying schema metadata
// so far. The golden test runs in "subset" mode: every generated entry must match
// the hand-written capabilities.json exactly, while entries not yet migrated are
// simply absent from the generated set and ignored. As more blocks are migrated,
// this list grows; at full migration the check flips to whole-file equality.
var migratedBlocks = []string{
	"set-payload", "set-variable", "delete-variable", "multi-transform",
	"template-resource", "flow-ref", "object-read", "object-write", "object-delete",
	"invalidate-cache", "clear-agent-memory", "log", "sql", "queue-dispatch",
	"publish-event", "jwt-validate", "ai-mapping",
	"slack-send-message", "slack-verify-request", "slack-event",
	"slack-lookup-user", "slack-add-reaction", "slack-update-message",
	"notion-retrieve-page", "notion-query-datasource", "notion-retrieve-blocks",
	"notion-page-to-markdown", "notion-verify-request", "notion-event",
	"rest", "if", "handle-errors", "fork", "switch", "foreach", "enrich",
	"validate", "ai-retry", "ai-router", "ai-agent", "mcp-router", "cache-scope",
}
var migratedConnectors = []string{
	"http-client", "http", "logger", "database", "queue", "events", "cron",
	"llm-anthropic", "llm-openai", "llm-gemini", "slack", "notion",
}

func TestGeneratedSchemaMatchesCapabilitiesJSON(t *testing.T) {
	caps, err := schema.Generate(core.DefaultSchemaRegistry())
	if err != nil {
		t.Fatalf("generate schema: %v", err)
	}
	live := loadCapabilities(t)

	liveBlocks := make(map[string]schema.Block, len(live.Blocks))
	for _, b := range live.Blocks {
		liveBlocks[b.Type] = b
	}
	liveConnectors := make(map[string]schema.Connector, len(live.Connectors))
	for _, c := range live.Connectors {
		liveConnectors[c.Type] = c
	}

	genBlocks := make(map[string]schema.Block, len(caps.Blocks))
	for _, b := range caps.Blocks {
		genBlocks[b.Type] = b
		lb, ok := liveBlocks[b.Type]
		if !ok {
			t.Errorf("generated block %q has no entry in capabilities.json", b.Type)
			continue
		}
		if g, w := canonical(t, b), canonical(t, lb); g != w {
			t.Errorf("block %q differs from capabilities.json:\ngenerated:\n%s\nwant:\n%s", b.Type, g, w)
		}
	}
	genConnectors := make(map[string]schema.Connector, len(caps.Connectors))
	for _, c := range caps.Connectors {
		genConnectors[c.Type] = c
		lc, ok := liveConnectors[c.Type]
		if !ok {
			t.Errorf("generated connector %q has no entry in capabilities.json", c.Type)
			continue
		}
		if g, w := canonical(t, c), canonical(t, lc); g != w {
			t.Errorf("connector %q differs from capabilities.json:\ngenerated:\n%s\nwant:\n%s", c.Type, g, w)
		}
	}

	// Guard against a block silently losing its metadata (which would drop it
	// from the generated set and make the per-entry loop above vacuously pass).
	for _, typ := range migratedBlocks {
		if _, ok := genBlocks[typ]; !ok {
			t.Errorf("expected migrated block %q to be generated, but it is missing", typ)
		}
	}
	for _, typ := range migratedConnectors {
		if _, ok := genConnectors[typ]; !ok {
			t.Errorf("expected migrated connector %q to be generated, but it is missing", typ)
		}
	}
}

// loadCapabilities reads the editor's capabilities.json, located relative to this
// test file (runtime/cli -> repo root -> packages/...).
func loadCapabilities(t *testing.T) schema.Capabilities {
	t.Helper()
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	path := filepath.Join(filepath.Dir(testFile), "..", "..",
		"packages", "editor", "src", "app", "schema", "capabilities.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read capabilities.json: %v", err)
	}
	var caps schema.Capabilities
	if err := json.Unmarshal(data, &caps); err != nil {
		t.Fatalf("parse capabilities.json: %v", err)
	}
	return caps
}

// canonical renders a value the same way on both sides so number-type and HTML
// escaping differences from unmarshalling the file don't cause false mismatches.
func canonical(t *testing.T, v any) string {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return buf.String()
}
