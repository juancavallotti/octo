package ai

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
)

// evalFrom compiles an attachments expression over a `body` variable and runs it
// against one, which is how every flow states this setting.
func evalFrom(t *testing.T, expression string, body any) ([]core.LLMAttachment, error) {
	t.Helper()
	program, err := expr.Compile(expression, "body")
	if err != nil {
		t.Fatalf("compile %q: %v", expression, err)
	}
	return evalAttachments(program, map[string]any{"body": body})
}

func TestEvalAttachmentsReadsAList(t *testing.T) {
	got, err := evalFrom(t, "body.files", map[string]any{"files": []any{
		map[string]any{
			"mimeType": "image/png",
			"data":     base64.StdEncoding.EncodeToString([]byte("png")),
			"name":     "shot.png",
		},
		map[string]any{
			"mimeType": "application/pdf",
			"data":     base64.StdEncoding.EncodeToString([]byte("%PDF")),
		},
	}})
	if err != nil {
		t.Fatalf("evalAttachments: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("attachments = %d, want 2", len(got))
	}
	if got[0].MimeType != "image/png" || string(got[0].Data) != "png" || got[0].Name != "shot.png" {
		t.Errorf("attachment 0 = %+v", got[0])
	}
	if got[1].Name != "" {
		t.Errorf("attachment 1 name = %q, want empty", got[1].Name)
	}
}

// A browser hands out data URLs, so one is unwrapped here rather than in every
// flow's CEL.
func TestEvalAttachmentsUnwrapsADataURL(t *testing.T) {
	url := "data:image/webp;base64," + base64.StdEncoding.EncodeToString([]byte("webp"))
	got, err := evalFrom(t, "body.files", map[string]any{"files": []any{
		map[string]any{"data": url},
	}})
	if err != nil {
		t.Fatalf("evalAttachments: %v", err)
	}
	if got[0].MimeType != "image/webp" || string(got[0].Data) != "webp" {
		t.Errorf("attachment = %+v, want the type and bytes off the URL", got[0])
	}
}

// An author who wrote a mimeType meant the one they wrote.
func TestEvalAttachmentsPrefersAStatedMimeType(t *testing.T) {
	url := "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString([]byte("x"))
	got, err := evalFrom(t, "body.files", map[string]any{"files": []any{
		map[string]any{"mimeType": "image/png", "data": url},
	}})
	if err != nil {
		t.Fatalf("evalAttachments: %v", err)
	}
	if got[0].MimeType != "image/png" {
		t.Errorf("mimeType = %q, want the stated one", got[0].MimeType)
	}
}

// An expression guarded with has() answers null for a message that carries
// nothing, which is the ordinary case and not a mistake.
func TestEvalAttachmentsTolerateAbsence(t *testing.T) {
	for name, expression := range map[string]string{
		"guarded": `has(body.files) ? body.files : []`,
		"empty":   `[]`,
	} {
		got, err := evalFrom(t, expression, map[string]any{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != nil {
			t.Errorf("%s = %+v, want nil", name, got)
		}
	}
}

func TestEvalAttachmentsWithoutAProgramYieldNothing(t *testing.T) {
	got, err := evalAttachments(nil, nil)
	if got != nil || err != nil {
		t.Errorf("evalAttachments(nil) = %+v, %v; want nil, nil", got, err)
	}
}

// Every failure names the entry it is about: a list of five with one bad member
// is otherwise a hunt.
func TestEvalAttachmentsRejectBadEntries(t *testing.T) {
	cases := map[string]struct {
		body    any
		mention string
	}{
		"not a list": {
			body:    map[string]any{"files": "nope"},
			mention: "want a list",
		},
		"entry not a map": {
			body:    map[string]any{"files": []any{"nope"}},
			mention: "attachment 0",
		},
		"data missing": {
			body:    map[string]any{"files": []any{map[string]any{"mimeType": "image/png"}}},
			mention: "must be a base64 string",
		},
		"data not a string": {
			body:    map[string]any{"files": []any{map[string]any{"data": 42}}},
			mention: "must be a base64 string",
		},
		"mimeType missing": {
			body:    map[string]any{"files": []any{map[string]any{"data": "cG5n"}}},
			mention: "mimeType is required",
		},
		"data not base64": {
			body: map[string]any{"files": []any{
				map[string]any{"mimeType": "image/png", "data": "not base64!!"},
			}},
			mention: "decoding data",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := evalFrom(t, "body.files", tc.body)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.mention) {
				t.Errorf("error %q does not mention %q", err, tc.mention)
			}
		})
	}
}

// The second entry is the one that fails, and the error has to say so.
func TestEvalAttachmentsNameTheOffendingIndex(t *testing.T) {
	_, err := evalFrom(t, "body.files", map[string]any{"files": []any{
		map[string]any{"mimeType": "image/png", "data": "cG5n"},
		map[string]any{"mimeType": "image/png", "data": "not base64!!"},
	}})
	if err == nil || !strings.Contains(err.Error(), "attachment 1") {
		t.Errorf("error = %v, want one naming attachment 1", err)
	}
}

// Media is not billed by its size, so it is not estimated by its size either.
// Counting attachment bytes at chars/4 would put a one-megabyte image at 175,000
// tokens; counting them at zero breaks the context meter's fitted scale, because
// attachments ride one turn and the measured prompt then drops while the estimate
// does not move.
func TestEstimateTokensCountMediaAtItsOwnRate(t *testing.T) {
	const size = 750 * 1000 // a round number of media-tokens: 1000
	withMedia := []core.LLMMessage{{
		Role:        core.LLMRoleUser,
		Attachments: []core.LLMAttachment{{MimeType: "image/png", Data: make([]byte, size)}},
	}}
	got := estimateTokens(withMedia)
	if got != 1000 {
		t.Errorf("estimateTokens = %d, want 1000", got)
	}
	// And far below what the same bytes of prose would estimate at.
	asText := estimateTokens([]core.LLMMessage{{
		Role: core.LLMRoleUser, Text: strings.Repeat("x", size),
	}})
	if got >= asText/100 {
		t.Errorf("media estimated at %d against %d for the same bytes of text: too close", got, asText)
	}
}

func TestEstimateTokensIgnoreAMessageWithNoAttachments(t *testing.T) {
	plain := []core.LLMMessage{{Role: core.LLMRoleUser, Text: "hello"}}
	if got, want := estimateTokens(plain), len("hello")/charsPerToken; got != want {
		t.Errorf("estimateTokens = %d, want %d", got, want)
	}
}
