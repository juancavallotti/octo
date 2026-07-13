package notion

import (
	"context"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

// blocksBody wraps a JSON blocks array under "results", matching Notion's
// block-children response, and returns it as the message body.
func blocksBody(t *testing.T, resultsJSON string) *types.Message {
	t.Helper()
	msg := blockMessage(t, nil)
	if err := msg.SetBodyJSON([]byte(`{"results":` + resultsJSON + `}`)); err != nil {
		t.Fatalf("SetBodyJSON: %v", err)
	}
	return msg
}

func TestMarkdownRendersCommonBlocks(t *testing.T) {
	results := `[
		{"type":"heading_1","heading_1":{"rich_text":[{"plain_text":"Title","annotations":{}}]}},
		{"type":"paragraph","paragraph":{"rich_text":[
			{"plain_text":"hello ","annotations":{}},
			{"plain_text":"world","annotations":{"bold":true}}
		]}},
		{"type":"bulleted_list_item","bulleted_list_item":{"rich_text":[{"plain_text":"one","annotations":{}}]}},
		{"type":"to_do","to_do":{"checked":true,"rich_text":[{"plain_text":"done","annotations":{}}]}},
		{"type":"code","code":{"language":"go","rich_text":[{"plain_text":"x := 1","annotations":{}}]}},
		{"type":"divider","divider":{}}
	]`
	proc, err := newPageToMarkdown(types.Settings{}, blockDeps(t, ""))
	if err != nil {
		t.Fatalf("newPageToMarkdown: %v", err)
	}

	out, err := proc.Process(context.Background(), blocksBody(t, results))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	ct, md, ok := out.RawBody()
	if !ok {
		t.Fatal("expected raw content output")
	}
	if ct != markdownContentType {
		t.Errorf("content type = %q, want %q", ct, markdownContentType)
	}
	for _, want := range []string{"# Title", "hello **world**", "- one", "- [x] done", "```go\nx := 1\n```", "---"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q; got:\n%s", want, md)
		}
	}
}

func TestMarkdownStoresInResultVar(t *testing.T) {
	results := `[{"type":"paragraph","paragraph":{"rich_text":[{"plain_text":"body text","annotations":{}}]}}]`
	proc, err := newPageToMarkdown(types.Settings{"resultVar": "md"}, blockDeps(t, ""))
	if err != nil {
		t.Fatalf("newPageToMarkdown: %v", err)
	}

	msg := blocksBody(t, results)
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if _, _, ok := out.RawBody(); ok {
		t.Error("did not expect raw content when resultVar is set")
	}
	if got, _ := out.Variables.String("md"); got != "body text" {
		t.Errorf("md = %q, want \"body text\"", got)
	}
	// The body is left untouched (still the {results:[...]} object).
	if _, ok := out.Body.(map[string]any); !ok {
		t.Errorf("expected the body left intact, got %T", out.Body)
	}
}

func TestMarkdownSkipsUnknownBlocks(t *testing.T) {
	results := `[
		{"type":"unsupported_widget","unsupported_widget":{}},
		{"type":"paragraph","paragraph":{"rich_text":[{"plain_text":"kept","annotations":{}}]}}
	]`
	proc, err := newPageToMarkdown(types.Settings{}, blockDeps(t, ""))
	if err != nil {
		t.Fatalf("newPageToMarkdown: %v", err)
	}
	out, err := proc.Process(context.Background(), blocksBody(t, results))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	_, md, _ := out.RawBody()
	if md != "kept" {
		t.Errorf("markdown = %q, want \"kept\" (unknown block skipped)", md)
	}
}

func TestMarkdownErrorsOnNonList(t *testing.T) {
	proc, err := newPageToMarkdown(types.Settings{"source": "body.notAList"}, blockDeps(t, ""))
	if err != nil {
		t.Fatalf("newPageToMarkdown: %v", err)
	}
	msg := blockMessage(t, map[string]any{"notAList": "oops"})
	if _, err := proc.Process(context.Background(), msg); err == nil {
		t.Error("expected an error when the source is not a list")
	}
}
