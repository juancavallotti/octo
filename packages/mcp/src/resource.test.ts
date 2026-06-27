import { describe, expect, it } from "vitest";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { registerRuntimeSchemaResource, RUNTIME_SCHEMA_URI } from "./resource";
import { registerPrompts } from "./prompts";
import type { OctoMcpConfig } from "./backend";

const noopStore: OctoMcpConfig["store"] = {
  list: async () => [],
  get: async () => ({ id: "", name: "", definition: "" }),
  create: async () => ({ id: "", name: "", definition: "" }),
  update: async () => ({ id: "", name: "", definition: "" }),
};

async function connect(runtimeSchema: unknown): Promise<Client> {
  const config: OctoMcpConfig = {
    store: noopStore,
    validate: () => ({ valid: true, errors: [] }),
    runtimeSchema,
  };
  const server = new McpServer({ name: "octo-test", version: "0.0.0" });
  registerRuntimeSchemaResource(server, config);
  registerPrompts(server);
  const [clientT, serverT] = InMemoryTransport.createLinkedPair();
  const client = new Client({ name: "test-client", version: "0.0.0" });
  await Promise.all([server.connect(serverT), client.connect(clientT)]);
  return client;
}

describe("runtime schema resource", () => {
  it("serves capabilities JSON at the schema URI", async () => {
    const schema = { blocks: [{ type: "log" }], connectors: [{ type: "http" }] };
    const client = await connect(schema);
    const listed = await client.listResources();
    expect(listed.resources.map((r) => r.uri)).toContain(RUNTIME_SCHEMA_URI);
    const read = await client.readResource({ uri: RUNTIME_SCHEMA_URI });
    const content = read.contents[0] as { mimeType: string; text: string };
    expect(content.mimeType).toBe("application/json");
    expect(JSON.parse(content.text)).toEqual(schema);
  });
});

describe("prompts", () => {
  it("exposes create-integration and integration-examples", async () => {
    const client = await connect({});
    const names = (await client.listPrompts()).prompts.map((p) => p.name);
    expect(names).toEqual(
      expect.arrayContaining(["create-integration", "integration-examples"]),
    );
  });

  it("weaves the goal into the create-integration guide and references the loop", async () => {
    const client = await connect({});
    const res = await client.getPrompt({
      name: "create-integration",
      arguments: { goal: "poll a weather API every minute" },
    });
    const text = (res.messages[0].content as { text: string }).text;
    expect(text).toContain("poll a weather API every minute");
    expect(text).toContain("can_start_integration");
    expect(text).toContain(RUNTIME_SCHEMA_URI);
  });

  it("returns runnable example definitions", async () => {
    const client = await connect({});
    const res = await client.getPrompt({ name: "integration-examples" });
    const text = (res.messages[0].content as { text: string }).text;
    expect(text).toContain("service:");
    expect(text).toContain("HTTP_PORT");
  });
});
