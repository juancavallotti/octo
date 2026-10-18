import { describe, expect, it } from "vitest";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { registerPrompts } from "./prompts";
import type { OctoMcpConfig } from "./backend";

/** Stand up a linked client/server with the prompts registered. */
async function connect(over: Partial<OctoMcpConfig> = {}): Promise<Client> {
  const config: OctoMcpConfig = {
    store: {
      list: async () => [],
      get: async () => ({ id: "", name: "", files: [] }),
      create: async () => ({ id: "", name: "", files: [] }),
      update: async () => ({ id: "", name: "", files: [] }),
    },
    validate: () => ({ valid: true, errors: [] }),
    runtimeSchema: {},
    ...over,
  };
  const server = new McpServer({ name: "octo-test", version: "0.0.0" });
  registerPrompts(server, config);
  const [clientT, serverT] = InMemoryTransport.createLinkedPair();
  const client = new Client({ name: "test-client", version: "0.0.0" });
  await Promise.all([server.connect(serverT), client.connect(clientT)]);
  return client;
}

/** The text of a prompt's single user message. */
async function getText(
  client: Client,
  name: string,
  args?: Record<string, string>,
): Promise<string> {
  const res = await client.getPrompt({ name, arguments: args ?? {} });
  return res.messages.map((m) => (m.content as { text: string }).text).join("\n");
}

describe("prompts", () => {
  it("lists both authoring prompts", async () => {
    const client = await connect();
    const { prompts } = await client.listPrompts();
    const names = prompts.map((p) => p.name);
    expect(names).toContain("create-integration");
    expect(names).toContain("write-effective-integrations");
  });

  it("write-effective-integrations covers the key design concepts", async () => {
    const client = await connect();
    const text = await getText(client, "write-effective-integrations");
    // Splitting flows, queues vs. topics, transforms, errors, concurrency, mermaid.
    expect(text).toContain("flow-ref");
    expect(text).toContain("queue-dispatch");
    expect(text).toContain("publish-event");
    expect(text).toContain("multi-transform");
    expect(text).toContain("handle-errors");
    expect(text).toMatch(/workers/);
    expect(text.toLowerCase()).toContain("mermaid");
  });

  it("weaves the focus argument into the guidance", async () => {
    const client = await connect();
    const text = await getText(client, "write-effective-integrations", {
      focus: "idempotency",
    });
    expect(text).toContain("idempotency");
  });

  it("includes the docs URL when the host configures one", async () => {
    const client = await connect({ docsUrl: "https://docs.example.com" });
    const text = await getText(client, "write-effective-integrations");
    expect(text).toContain("https://docs.example.com");
  });
});
