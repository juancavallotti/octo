import { describe, expect, it } from "vitest";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import {
  EXAMPLES_INDEX_URI,
  exampleUri,
  registerExampleResources,
  registerRuntimeSchemaResource,
  RUNTIME_SCHEMA_URI,
} from "./resource";
import { registerPrompts } from "./prompts";
import { EXAMPLES } from "./examples";
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
  registerExampleResources(server);
  registerPrompts(server, config);
  const [clientT, serverT] = InMemoryTransport.createLinkedPair();
  const client = new Client({ name: "test-client", version: "0.0.0" });
  await Promise.all([server.connect(serverT), client.connect(clientT)]);
  return client;
}

function textOf(read: { contents: unknown[] }): string {
  return (read.contents[0] as { text: string }).text;
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

describe("example resources", () => {
  it("lists an index plus one resource per example", async () => {
    const client = await connect({});
    const uris = (await client.listResources()).resources.map((r) => r.uri);
    expect(uris).toContain(EXAMPLES_INDEX_URI);
    for (const e of EXAMPLES) expect(uris).toContain(exampleUri(e.slug));
  });

  it("the index lists each example's blocks and resource URI", async () => {
    const client = await connect({});
    const index = JSON.parse(textOf(await client.readResource({ uri: EXAMPLES_INDEX_URI })));
    expect(index).toHaveLength(EXAMPLES.length);
    const builtins = index.find((e: { slug: string }) => e.slug === "builtins");
    expect(builtins.uri).toBe(exampleUri("builtins"));
    expect(builtins.blocks).toEqual(expect.arrayContaining(["foreach", "switch"]));
  });

  it("serves a self-describing YAML definition per example", async () => {
    const client = await connect({});
    const read = await client.readResource({ uri: exampleUri("http-orders") });
    const content = read.contents[0] as { mimeType: string; text: string };
    expect(content.mimeType).toBe("application/yaml");
    expect(content.text).toContain("Demonstrates:"); // header comment
    expect(content.text).toContain("flow-ref"); // the block it showcases
    expect(content.text).toContain("HTTP_PORT"); // networked
  });
});

describe("prompts", () => {
  it("exposes create-integration", async () => {
    const client = await connect({});
    const names = (await client.listPrompts()).prompts.map((p) => p.name);
    expect(names).toContain("create-integration");
  });

  it("weaves the goal into the guide and points at the schema + examples resources", async () => {
    const client = await connect({});
    const res = await client.getPrompt({
      name: "create-integration",
      arguments: { goal: "poll a weather API every minute" },
    });
    const text = (res.messages[0].content as { text: string }).text;
    expect(text).toContain("poll a weather API every minute");
    expect(text).toContain("can_start_integration");
    expect(text).toContain(RUNTIME_SCHEMA_URI);
    expect(text).toContain(EXAMPLES_INDEX_URI);
  });

  it("includes the docs URL only when the host configures one", async () => {
    const withoutDocs = await getGuide({});
    expect(withoutDocs).not.toContain("Reference documentation");

    const withDocs = await getGuide({ docsUrl: "https://docs.example.dev" });
    expect(withDocs).toContain("Reference documentation: https://docs.example.dev");
  });
});

/** Render the create-integration guide for a config (docsUrl etc.). */
async function getGuide(over: Partial<OctoMcpConfig>): Promise<string> {
  const config: OctoMcpConfig = {
    store: noopStore,
    validate: () => ({ valid: true, errors: [] }),
    runtimeSchema: {},
    ...over,
  };
  const server = new McpServer({ name: "octo-test", version: "0.0.0" });
  registerPrompts(server, config);
  const [clientT, serverT] = InMemoryTransport.createLinkedPair();
  const client = new Client({ name: "test-client", version: "0.0.0" });
  await Promise.all([server.connect(serverT), client.connect(clientT)]);
  const res = await client.getPrompt({
    name: "create-integration",
    arguments: {},
  });
  return (res.messages[0].content as { text: string }).text;
}
