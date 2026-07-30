// Package aiblocks provides leaf processor blocks powered by an LLM connector.
// The composite AI elements (ai-router, ai-agent, ai-retry) are built by the
// flow engine; the leaf elements that fit the block registry live here.
//
// Two leaves today: "ai-mapping" reshapes the message body to a target shape
// described by a prompt, optional input/output examples, and an optional output
// JSON Schema (validated); "ai-embed" turns text into embedding vectors stored in
// a variable. Blocks here bind to an LLM provider by name and type-assert to
// core.LLMClient — the shared interface — so they work with any configured
// provider (llm-anthropic, llm-openai, llm-gemini).
package aiblocks

// init is this module's manifest: the one place that says what importing this
// package puts into the runtime, in a deterministic order. Each block's own
// registration lives beside the block as a registerX function called from here.
func init() {
	registerMapping()
	registerEmbed()
}
