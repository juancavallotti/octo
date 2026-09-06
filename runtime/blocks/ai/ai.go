// Package ai holds the blocks that put a model in the flow: the LLM-driven
// composites (ai-router, ai-retry, ai-agent), the provider-agnostic leaves
// (ai-mapping, ai-embed, clear-agent-memory), and the mcp-router, which exposes
// a flow's tool branches over the Model Context Protocol and calls no model
// itself.
//
// None of them belongs to a provider. They bind to whatever LLM connector a
// flow names through the shared core.LLMClient / core.EmbedClient interfaces,
// which is why they live here rather than beside any one connector.
package ai

import (
	"reflect"

	"github.com/juancavallotti/octo/runtime/core"
)

// The block types this package registers.
const (
	blockKindAIRouter  = "ai-router"
	blockKindAIAgent   = "ai-agent"
	blockKindAIRetry   = "ai-retry"
	blockKindMCPRouter = "mcp-router"
)

// groupAILLM is the palette group the blocks fall under in the editor sidebar.
const groupAILLM = "AI & LLM"

// init is this module's manifest: the one place that says what the package puts
// into the block registry and the editor schema, in a deterministic order.
func init() {
	registerComposites()
	registerAIMapping()
	registerAIEmbed()
	registerClearAgentMemory()
}

// registerComposites registers the blocks that run sub-flows under a model's
// direction, plus the mcp-router that runs them under a client's.
func registerComposites() {
	core.MustRegisterBlock(blockKindAIRetry, newAIRetry)
	core.RegisterBlockMeta(core.BlockMeta{
		Type:     blockKindAIRetry,
		Label:    "AI Retry",
		Category: core.CategoryControlFlow,
		Group:    groupAILLM,
		Icon:     "RefreshCw",
		Description: "Run the process chain; on error, let the LLM inspect vars.error, revise the " +
			"message, and retry up to maxAttempts, then run the error chain.",
		Config: reflect.TypeFor[retrySettings](),
	})
	core.MustRegisterBlock(blockKindAIRouter, newAIRouter)
	core.RegisterBlockMeta(core.BlockMeta{
		Type:     blockKindAIRouter,
		Label:    "AI Router",
		Category: core.CategoryControlFlow,
		Group:    groupAILLM,
		Icon:     "Route",
		Description: "Route the message to a named branch the LLM chooses after inspecting it; the " +
			"default path is the guardrail taken when it is not confident.",
		Config: reflect.TypeFor[routerSettings](),
	})
	core.MustRegisterBlock(blockKindAIAgent, newAIAgent)
	core.RegisterBlockMeta(core.BlockMeta{
		Type:     blockKindAIAgent,
		Label:    "AI Agent",
		Category: core.CategoryControlFlow,
		Group:    groupAILLM,
		Icon:     "Bot",
		Description: "Let the LLM call branches as tools in a loop to accomplish a task; tool runs " +
			"share accumulating variables. The default path is the guardrail.",
		Config: reflect.TypeFor[agentSettings](),
	})
	core.MustRegisterBlock(blockKindMCPRouter, newMCPRouter)
	core.RegisterBlockMeta(core.BlockMeta{
		Type:     blockKindMCPRouter,
		Label:    "MCP Router",
		Category: core.CategoryControlFlow,
		Group:    groupAILLM,
		Icon:     "Mcp",
		Description: "Turn a flow into a stateless MCP server behind an HTTP source: advertise tool " +
			"flows as MCP tools, template resources as MCP resources and prompts, and route " +
			"tools/call to the matching flow. Calls no LLM.",
		Config: reflect.TypeFor[mcpRouterSettings](),
	})
}
