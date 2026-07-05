import { describe, expect, it } from "vitest";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { registerDocsTools } from "./docs";
import { EXAMPLES } from "../examples";
import type { OctoMcpConfig } from "../backend";

const noopStore: OctoMcpConfig["store"] = {
  list: async () => [],
  get: async () => ({ id: "", name: "", definition: "" }),
  create: async () => ({ id: "", name: "", definition: "" }),
  update: async () => ({ id: "", name: "", definition: "" }),
};

const SCHEMA = {
  blocks: [
    { type: "log", label: "Log", category: "processor", description: "Log a line.", fields: [{ name: "message" }] },
    { type: "set-payload", label: "Set Payload", category: "processor", fields: [] },
  ],
  connectors: [
    { type: "http", label: "HTTP Server", description: "HTTP server.", settings: [{ name: "port" }], sources: [{ type: "http" }] },
  ],
};

async function connect(runtimeSchema: unknown = SCHEMA): Promise<Client> {
  const config: OctoMcpConfig = {
    store: noopStore,
    validate: () => ({ valid: true, errors: [] }),
    runtimeSchema,
  };
  const server = new McpServer({ name: "octo-test", version: "0.0.0" });
  registerDocsTools(server, config);
  const [clientT, serverT] = InMemoryTransport.createLinkedPair();
  const client = new Client({ name: "test-client", version: "0.0.0" });
  await Promise.all([server.connect(serverT), client.connect(clientT)]);
  return client;
}

async function call(
  client: Client,
  name: string,
  args: Record<string, unknown> = {},
): Promise<{ text: string; isError: boolean }> {
  const res = (await client.callTool({ name, arguments: args })) as {
    content: { text: string }[];
    isError?: boolean;
  };
  return { text: res.content[0].text, isError: res.isError === true };
}

describe("getSchema tool", () => {
  it("indexes every block and connector tagged with its kind", async () => {
    const client = await connect();
    const index = JSON.parse((await call(client, "getSchema")).text);
    expect(index).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ type: "log", kind: "block" }),
        expect.objectContaining({ type: "set-payload", kind: "block" }),
        expect.objectContaining({ type: "http", kind: "connector" }),
      ]),
    );
    // The index is compact — it omits the per-element fields/settings.
    expect(index.find((e: { type: string }) => e.type === "log")).not.toHaveProperty("fields");
  });

  it("returns a block's full spec including its fields", async () => {
    const client = await connect();
    const spec = JSON.parse((await call(client, "getSchema", { elementName: "log" })).text);
    expect(spec.kind).toBe("block");
    expect(spec.fields).toEqual([{ name: "message" }]);
  });

  it("returns a connector's full spec tagged as a connector", async () => {
    const client = await connect();
    const spec = JSON.parse((await call(client, "getSchema", { elementName: "http" })).text);
    expect(spec.kind).toBe("connector");
    expect(spec.settings).toEqual([{ name: "port" }]);
  });

  it("errors on an unknown element", async () => {
    const client = await connect();
    const res = await call(client, "getSchema", { elementName: "nope" });
    expect(res.isError).toBe(true);
    expect(res.text).toContain("Unknown element");
  });
});

describe("getExamples tool", () => {
  it("indexes every example with its blocks", async () => {
    const client = await connect();
    const index = JSON.parse((await call(client, "getExamples")).text);
    expect(index).toHaveLength(EXAMPLES.length);
    const builtins = index.find((e: { slug: string }) => e.slug === "builtins");
    expect(builtins.blocks).toEqual(expect.arrayContaining(["foreach", "switch"]));
    // The index omits the full definition.
    expect(builtins).not.toHaveProperty("definition");
  });

  it("returns a named example's full YAML definition", async () => {
    const client = await connect();
    const example = JSON.parse((await call(client, "getExamples", { exampleName: "http-orders" })).text);
    expect(example.slug).toBe("http-orders");
    expect(example.definition).toContain("flow-ref");
  });

  it("errors on an unknown example", async () => {
    const client = await connect();
    const res = await call(client, "getExamples", { exampleName: "nope" });
    expect(res.isError).toBe(true);
    expect(res.text).toContain("Unknown example");
  });
});

describe("getCelFunctions tool", () => {
  it("lists the CEL variables and functions", async () => {
    const client = await connect();
    const cat = JSON.parse((await call(client, "getCelFunctions")).text);
    expect(cat.variables.map((v: { name: string }) => v.name)).toEqual(
      expect.arrayContaining(["body", "vars", "env", "now"]),
    );
    expect(cat.functions.map((f: { name: string }) => f.name)).toEqual(
      expect.arrayContaining(["toJson", "fromJson", "fromFormData", "templateResource"]),
    );
  });

  it("returns one function's spec by name", async () => {
    const client = await connect();
    const fn = JSON.parse((await call(client, "getCelFunctions", { functionName: "fromFormData" })).text);
    expect(fn.name).toBe("fromFormData");
    expect(fn.signature).toContain("fromFormData");
  });

  it("errors on an unknown function", async () => {
    const client = await connect();
    const res = await call(client, "getCelFunctions", { functionName: "nope" });
    expect(res.isError).toBe(true);
    expect(res.text).toContain("Unknown CEL function");
  });
});
