package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/internal/pool"
	"github.com/juancavallotti/octo/types"
)

// depsRes builds block deps carrying only an in-memory resource loader (an
// mcp-router needs no connector).
func depsRes(res mapResources) core.BlockDeps {
	return core.BlockDeps{Resources: res}
}

// mcpResources is the template set the router tests serve resources/prompts from.
func mcpResources() mapResources {
	return mapResources{
		"guide": "The operator guide.",
		"greet": "Hello {{ body.who }}",
	}
}

// mcpRouterConfig builds an mcp-router with one tool, one resource, and one prompt.
func mcpRouterConfig() types.BlockConfig {
	return types.BlockConfig{
		Type: "mcp-router", Name: "test-server",
		Tools: []types.ToolConfig{
			toolBranch("echo", "echoes a fixed result", types.Settings{"result": `{"echoed":true}`}),
		},
		Resources: []types.MCPResourceConfig{
			{URI: "octo://guide", Name: "guide", Description: "the operator guide", Resource: "guide"},
		},
		Prompts: []types.MCPPromptConfig{
			{Name: "greet", Description: "a greeting", Resource: "greet",
				Arguments: []types.MCPPromptArg{{Name: "who", Description: "who to greet", Required: true}}},
		},
	}
}

// mcpCall sends one JSON-RPC request through the router and decodes the response
// body as a generic map.
func mcpCall(t *testing.T, proc core.MessageProcessor, request string) map[string]any {
	t.Helper()
	out, err := proc.Process(context.Background(), newMessageBody(t, request))
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	raw, err := out.BodyJSON()
	if err != nil {
		t.Fatalf("encode response: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode response %s: %v", raw, err)
	}
	return resp
}

// resultOf returns the "result" object of a JSON-RPC response, failing if the
// response carries an error instead.
func resultOf(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	if e, ok := resp["error"]; ok {
		t.Fatalf("unexpected error response: %v", e)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("response has no result object: %v", resp)
	}
	return result
}

func TestMCPRouterInitialize(t *testing.T) {
	proc := mustBuildAI(t, agentRegistry(&[]any{}), depsRes(mcpResources()), mcpRouterConfig())
	resp := mcpCall(t, proc, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	result := resultOf(t, resp)

	if result["protocolVersion"] != "2025-06-18" {
		t.Errorf("protocolVersion = %v, want the client's echoed version", result["protocolVersion"])
	}
	info := result["serverInfo"].(map[string]any)
	if info["name"] != "test-server" {
		t.Errorf("serverInfo.name = %v, want test-server", info["name"])
	}
	caps := result["capabilities"].(map[string]any)
	for _, key := range []string{"tools", "resources", "prompts"} {
		if _, ok := caps[key]; !ok {
			t.Errorf("capabilities missing %q: %v", key, caps)
		}
	}
}

func TestMCPRouterInitializeDefaultsVersion(t *testing.T) {
	proc := mustBuildAI(t, agentRegistry(&[]any{}), depsRes(mcpResources()), mcpRouterConfig())
	resp := mcpCall(t, proc, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if resultOf(t, resp)["protocolVersion"] != mcpProtocolVersion {
		t.Errorf("protocolVersion should default when the client sends none")
	}
}

func TestMCPRouterToolsListAndCall(t *testing.T) {
	var seen []any
	proc := mustBuildAI(t, agentRegistry(&seen), depsRes(mcpResources()), mcpRouterConfig())

	list := resultOf(t, mcpCall(t, proc, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	tools := list["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "echo" {
		t.Fatalf("tools/list = %v, want the echo tool", tools)
	}
	if _, ok := tools[0].(map[string]any)["inputSchema"]; !ok {
		t.Errorf("tool is missing its inputSchema: %v", tools[0])
	}

	call := resultOf(t, mcpCall(t, proc,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"x":1}}}`))
	if call["isError"] != false {
		t.Errorf("tools/call isError = %v, want false", call["isError"])
	}
	text := call["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "echoed") {
		t.Errorf("tools/call text = %q, want the flow's output body", text)
	}
	// The tool branch ran, receiving the arguments as its body.
	if len(seen) != 1 || seen[0] != `{"x":1}` {
		t.Errorf("tool branch saw %v, want the call arguments", seen)
	}
}

func TestMCPRouterToolsCallUnknownIsError(t *testing.T) {
	proc := mustBuildAI(t, agentRegistry(&[]any{}), depsRes(mcpResources()), mcpRouterConfig())
	call := resultOf(t, mcpCall(t, proc,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nope","arguments":{}}}`))
	if call["isError"] != true {
		t.Errorf("unknown tool should be an isError result: %v", call)
	}
}

func TestMCPRouterResources(t *testing.T) {
	proc := mustBuildAI(t, agentRegistry(&[]any{}), depsRes(mcpResources()), mcpRouterConfig())

	list := resultOf(t, mcpCall(t, proc, `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`))
	resources := list["resources"].([]any)
	if len(resources) != 1 || resources[0].(map[string]any)["uri"] != "octo://guide" {
		t.Fatalf("resources/list = %v", resources)
	}

	read := resultOf(t, mcpCall(t, proc,
		`{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"octo://guide"}}`))
	contents := read["contents"].([]any)[0].(map[string]any)
	if contents["text"] != "The operator guide." {
		t.Errorf("resources/read text = %v, want the rendered resource", contents["text"])
	}
	if contents["uri"] != "octo://guide" {
		t.Errorf("resources/read uri = %v", contents["uri"])
	}
}

func TestMCPRouterResourceReadUnknown(t *testing.T) {
	proc := mustBuildAI(t, agentRegistry(&[]any{}), depsRes(mcpResources()), mcpRouterConfig())
	resp := mcpCall(t, proc,
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"octo://missing"}}`)
	if _, ok := resp["error"]; !ok {
		t.Errorf("reading an unknown resource should be a JSON-RPC error: %v", resp)
	}
}

func TestMCPRouterPrompts(t *testing.T) {
	proc := mustBuildAI(t, agentRegistry(&[]any{}), depsRes(mcpResources()), mcpRouterConfig())

	list := resultOf(t, mcpCall(t, proc, `{"jsonrpc":"2.0","id":1,"method":"prompts/list"}`))
	prompts := list["prompts"].([]any)
	if len(prompts) != 1 {
		t.Fatalf("prompts/list = %v", prompts)
	}
	args := prompts[0].(map[string]any)["arguments"].([]any)
	if args[0].(map[string]any)["name"] != "who" {
		t.Errorf("prompt argument metadata missing: %v", args)
	}

	// prompts/get renders the template with the supplied arguments as the body.
	get := resultOf(t, mcpCall(t, proc,
		`{"jsonrpc":"2.0","id":2,"method":"prompts/get","params":{"name":"greet","arguments":{"who":"World"}}}`))
	msg := get["messages"].([]any)[0].(map[string]any)
	content := msg["content"].(map[string]any)
	if content["text"] != "Hello World" {
		t.Errorf("prompts/get text = %v, want the rendered template", content["text"])
	}
	if msg["role"] != "user" {
		t.Errorf("prompts/get role = %v, want user", msg["role"])
	}
}

func TestMCPRouterUnknownMethod(t *testing.T) {
	proc := mustBuildAI(t, agentRegistry(&[]any{}), depsRes(mcpResources()), mcpRouterConfig())
	resp := mcpCall(t, proc, `{"jsonrpc":"2.0","id":1,"method":"does/notexist"}`)
	e, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("unknown method should return an error: %v", resp)
	}
	if int(e["code"].(float64)) != jsonrpcMethodNotFound {
		t.Errorf("error code = %v, want %d", e["code"], jsonrpcMethodNotFound)
	}
}

func TestMCPRouterNotificationDropsWith202(t *testing.T) {
	proc := mustBuildAI(t, agentRegistry(&[]any{}), depsRes(mcpResources()), mcpRouterConfig())
	out, err := proc.Process(context.Background(),
		newMessageBody(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	_, rawData, ok := out.RawBody()
	if !ok || rawData != "" {
		t.Errorf("notification should produce an empty raw body, got ok=%v data=%q", ok, rawData)
	}
	if code, ok := out.Variables.Int(mcpHTTPStatusVar); !ok || code != mcpNotificationStatus {
		t.Errorf("notification httpStatus = %v (ok=%v), want %d", code, ok, mcpNotificationStatus)
	}
}

func TestMCPRouterBuildValidation(t *testing.T) {
	reg := agentRegistry(&[]any{})
	deps := depsRes(mcpResources())
	build := func(cfg types.BlockConfig) error {
		_, err := (&builder{reg: reg, pool: pool.New(0, 0), deps: deps}).block(cfg)
		return err
	}

	if err := build(types.BlockConfig{Type: "mcp-router"}); err == nil {
		t.Error("expected error with no tools, resources, or prompts")
	}
	dupResource := mcpRouterConfig()
	dupResource.Resources = append(dupResource.Resources,
		types.MCPResourceConfig{URI: "octo://guide", Name: "dup", Resource: "guide"})
	if err := build(dupResource); err == nil {
		t.Error("expected error with a duplicate resource uri")
	}
	noPromptResource := mcpRouterConfig()
	noPromptResource.Prompts[0].Resource = ""
	if err := build(noPromptResource); err == nil {
		t.Error("expected error with a prompt missing its resource")
	}
	noResourceRef := mcpRouterConfig()
	noResourceRef.Resources[0].Resource = ""
	if err := build(noResourceRef); err == nil {
		t.Error("expected error with a resource missing its resource ref")
	}
}
