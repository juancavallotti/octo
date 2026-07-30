// This file provides the "notion-page-to-markdown" block: a pure transformer that
// renders a Notion blocks array (as returned by GET /blocks/{id}/children under
// "results") into markdown. It makes no API call — fetch the blocks first (e.g.
// with the rest block) and point this block's source at them. By default it
// replaces the message with the rendered markdown as raw content (text/markdown);
// with resultVar set it stores the markdown in a variable and leaves the body.
package notion

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

func registerPageToMarkdown() {
	core.MustRegisterBlock("notion-page-to-markdown", newPageToMarkdown)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "notion-page-to-markdown",
		Label:    "Notion Page to Markdown",
		Category: core.CategoryProcessor,
		Description: "Render a Notion blocks array (as returned under results by the block-children " +
			"endpoint) into markdown. Emits raw content, or stores the markdown in a variable.",
		Config: reflect.TypeFor[markdownSettings](),
	})
}

const (
	// defaultSource is the CEL expression for the blocks array, matching the shape
	// of Notion's block-children response ({results: [...]}).
	defaultSource = "body.results"
	// markdownContentType labels the raw content the block produces.
	markdownContentType = "text/markdown"
)

// markdownSettings is the notion-page-to-markdown block's typed configuration.
type markdownSettings struct {
	// CEL expression producing the blocks array to render.
	Source string `json:"source" octo:"label=Source,type=cel,default=body.results"`
	// When set, store the markdown here and leave the body; when empty, replace the
	// body with the markdown as raw content.
	ResultVar string `json:"resultVar" octo:"label=Result variable"`
}

// markdownProcessor renders a Notion blocks array to markdown.
type markdownProcessor struct {
	source    *expr.Program
	resultVar string
	env       map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newPageToMarkdown(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg markdownSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	source, err := compileRequired(deps.Resources, "notion-page-to-markdown", "source",
		orDefault(cfg.Source, defaultSource))
	if err != nil {
		return nil, err
	}
	return &markdownProcessor{
		source:    source,
		resultVar: cfg.ResultVar,
		env:       expr.EnvActivation(deps.Env),
	}, nil
}

// Process resolves the blocks array, renders it to markdown, and either stores it
// in the result variable or replaces the body with it as raw content.
func (p *markdownProcessor) Process(_ context.Context, msg *types.Message) (*types.Message, error) {
	value, err := p.source.Eval(expr.MessageActivation(msg, p.env))
	if err != nil {
		return nil, fmt.Errorf("notion-page-to-markdown source: %w", err)
	}
	blocks, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("notion-page-to-markdown: source must be a list of blocks, got %T", value)
	}

	markdown := renderBlocks(blocks)
	if p.resultVar != "" {
		msg.Variables.Set(p.resultVar, markdown)
		return msg, nil
	}
	msg.SetRawBody(markdownContentType, markdown)
	return msg, nil
}

// renderBlocks renders a top-level Notion blocks array to markdown, joining blocks
// with a blank line. Nested children (has_children) are not fetched here — a block
// only renders what the given array contains (expand later as needed).
func renderBlocks(blocks []any) string {
	rendered := make([]string, 0, len(blocks))
	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if line, ok := renderBlock(block); ok {
			rendered = append(rendered, line)
		}
	}
	return strings.Join(rendered, "\n\n")
}

// blockPrefixes maps the Notion block types that render as a single prefixed line
// of rich text to their markdown prefix. Types needing extra structure (to_do,
// code, divider) are handled explicitly in renderBlock.
var blockPrefixes = map[string]string{
	"paragraph":          "",
	"heading_1":          "# ",
	"heading_2":          "## ",
	"heading_3":          "### ",
	"bulleted_list_item": "- ",
	"numbered_list_item": "1. ",
	"quote":              "> ",
	"callout":            "> ",
}

// renderBlock renders a single Notion block to markdown, returning ok=false for a
// type it does not handle so the caller skips it. A block's rich_text lives under
// the object keyed by its "type".
func renderBlock(block map[string]any) (string, bool) {
	blockType, _ := block["type"].(string)
	content, _ := block[blockType].(map[string]any)

	if prefix, ok := blockPrefixes[blockType]; ok {
		return prefix + richText(content), true
	}

	switch blockType {
	case "to_do":
		box := "[ ]"
		if checked, _ := content["checked"].(bool); checked {
			box = "[x]"
		}
		return "- " + box + " " + richText(content), true
	case "code":
		lang, _ := content["language"].(string)
		return "```" + lang + "\n" + richText(content) + "\n```", true
	case "divider":
		return "---", true
	default:
		return "", false
	}
}

// richText renders a block's rich_text array to markdown, concatenating each
// span's text with its bold/italic/strikethrough/code annotations applied.
func richText(content map[string]any) string {
	spans, ok := content["rich_text"].([]any)
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, raw := range spans {
		span, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		text, _ := span["plain_text"].(string)
		if text == "" {
			continue
		}
		b.WriteString(annotate(text, span["annotations"]))
	}
	return b.String()
}

// annotate wraps text in the markdown for its Notion annotations. Code is applied
// innermost, then strikethrough, italic, and bold outermost, so nested markers
// nest cleanly.
func annotate(text string, raw any) string {
	ann, ok := raw.(map[string]any)
	if !ok {
		return text
	}
	if code, _ := ann["code"].(bool); code {
		text = "`" + text + "`"
	}
	if strike, _ := ann["strikethrough"].(bool); strike {
		text = "~~" + text + "~~"
	}
	if italic, _ := ann["italic"].(bool); italic {
		text = "*" + text + "*"
	}
	if bold, _ := ann["bold"].(bool); bold {
		text = "**" + text + "**"
	}
	return text
}
