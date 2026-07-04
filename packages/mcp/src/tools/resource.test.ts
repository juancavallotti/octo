import { describe, expect, it } from "vitest";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { registerResourceTools } from "./resource";
import type {
  IntegrationRecord,
  OctoMcpConfig,
  ResourceRecord,
  ResourceStore,
} from "../backend";

/** A minimal in-memory IntegrationStore exposing a single definition. */
function fakeStore(seed: IntegrationRecord[] = []): OctoMcpConfig["store"] {
  const rows = new Map(seed.map((r) => [r.id, { ...r }]));
  return {
    list: async () => [...rows.values()].map(({ id, name }) => ({ id, name })),
    get: async (id) => {
      const r = rows.get(id);
      if (!r) throw new Error(`no such integration: ${id}`);
      return { ...r };
    },
    create: async (name, definition) => {
      const r = { id: `id-${rows.size + 1}`, name, definition };
      rows.set(r.id, r);
      return { ...r };
    },
    update: async (id, name, definition) => {
      const r = rows.get(id)!;
      const next = { id, name: name ?? r.name, definition };
      rows.set(id, next);
      return { ...next };
    },
  };
}

/** A trivial in-memory ResourceStore (ignores integration partitioning). */
function fakeResourceStore(seed: ResourceRecord[] = []) {
  const rows = new Map(seed.map((r) => [r.id, { ...r }]));
  let seq = rows.size;
  const store: ResourceStore & { rows: Map<string, ResourceRecord> } = {
    rows,
    list: async (integrationId) =>
      [...rows.values()].filter((r) => r.integrationId === integrationId),
    get: async (integrationId, resourceId) => {
      const r = rows.get(resourceId);
      if (!r || r.integrationId !== integrationId) {
        throw new Error(`no such resource: ${resourceId}`);
      }
      return { ...r };
    },
    create: async (integrationId, kind, name, content) => {
      const r: ResourceRecord = {
        id: `r-${++seq}`,
        integrationId,
        kind: kind as ResourceRecord["kind"],
        name,
        content,
      };
      rows.set(r.id, r);
      return { ...r };
    },
    update: async (integrationId, resourceId, kind, name, content) => {
      const next: ResourceRecord = {
        id: resourceId,
        integrationId,
        kind: kind as ResourceRecord["kind"],
        name,
        content,
      };
      rows.set(resourceId, next);
      return { ...next };
    },
    remove: async (_integrationId, resourceId) => {
      rows.delete(resourceId);
    },
  };
  return store;
}

async function connect(config: OctoMcpConfig): Promise<Client> {
  const server = new McpServer({ name: "octo-test", version: "0.0.0" });
  registerResourceTools(server, config);
  const [clientT, serverT] = InMemoryTransport.createLinkedPair();
  const client = new Client({ name: "test-client", version: "0.0.0" });
  await Promise.all([server.connect(serverT), client.connect(clientT)]);
  return client;
}

function config(
  store: OctoMcpConfig["store"],
  resourceStore?: ResourceStore,
): OctoMcpConfig {
  return {
    store,
    resourceStore,
    validate: () => ({ valid: true, errors: [] }),
    runtimeSchema: {},
  };
}

function parse(res: CallToolResult): unknown {
  const block = (res.content as { type: string; text: string }[])[0];
  return JSON.parse(block.text);
}

const ENV_DEF = [
  "service:",
  "  name: Demo",
  "env:",
  "  - name: API_KEY",
  "    required: true",
  "  - name: REGION",
  "    default: us-east-1",
].join("\n");

describe("resource tools", () => {
  it("lists resources as { id, kind, name } (no content)", async () => {
    const store = fakeStore([{ id: "a", name: "Demo", definition: "x" }]);
    const rs = fakeResourceStore([
      { id: "r1", integrationId: "a", kind: "env", name: ".env.dev", content: "K=v" },
      { id: "r2", integrationId: "b", kind: "template", name: "t.tmpl", content: "hi" },
    ]);
    const client = await connect(config(store, rs));
    const res = (await client.callTool({
      name: "list_resources",
      arguments: { id: "a" },
    })) as CallToolResult;
    expect(parse(res)).toEqual([{ id: "r1", kind: "env", name: ".env.dev" }]);
  });

  it("opens a resource with its content", async () => {
    const store = fakeStore([{ id: "a", name: "Demo", definition: "x" }]);
    const rs = fakeResourceStore([
      { id: "r1", integrationId: "a", kind: "env", name: ".env.dev", content: "K=v" },
    ]);
    const client = await connect(config(store, rs));
    const res = (await client.callTool({
      name: "open_resource",
      arguments: { id: "a", resourceId: "r1" },
    })) as CallToolResult;
    expect(parse(res)).toMatchObject({ id: "r1", name: ".env.dev", content: "K=v" });
  });

  it("creates a resource", async () => {
    const store = fakeStore([{ id: "a", name: "Demo", definition: "x" }]);
    const rs = fakeResourceStore();
    const client = await connect(config(store, rs));
    const res = (await client.callTool({
      name: "create_resource",
      arguments: { id: "a", kind: "env", name: ".env.dev", content: "API_KEY=secret" },
    })) as CallToolResult;
    const created = parse(res) as ResourceRecord;
    expect(created).toMatchObject({ integrationId: "a", kind: "env", name: ".env.dev" });
    expect(rs.rows.get(created.id)?.content).toBe("API_KEY=secret");
  });

  it("updates content while keeping kind/name when omitted (merge)", async () => {
    const store = fakeStore([{ id: "a", name: "Demo", definition: "x" }]);
    const rs = fakeResourceStore([
      { id: "r1", integrationId: "a", kind: "env", name: ".env.dev", content: "old" },
    ]);
    const client = await connect(config(store, rs));
    const res = (await client.callTool({
      name: "update_resource",
      arguments: { id: "a", resourceId: "r1", content: "new" },
    })) as CallToolResult;
    expect(parse(res)).toMatchObject({
      id: "r1",
      kind: "env",
      name: ".env.dev",
      content: "new",
    });
  });

  it("deletes a resource", async () => {
    const store = fakeStore([{ id: "a", name: "Demo", definition: "x" }]);
    const rs = fakeResourceStore([
      { id: "r1", integrationId: "a", kind: "env", name: ".env.dev", content: "x" },
    ]);
    const client = await connect(config(store, rs));
    const res = (await client.callTool({
      name: "delete_resource",
      arguments: { id: "a", resourceId: "r1" },
    })) as CallToolResult;
    expect(parse(res)).toEqual({ deleted: true });
    expect(rs.rows.has("r1")).toBe(false);
  });

  it("lists declared env keys from the definition", async () => {
    const store = fakeStore([{ id: "a", name: "Demo", definition: ENV_DEF }]);
    const client = await connect(config(store, fakeResourceStore()));
    const res = (await client.callTool({
      name: "list_env_keys",
      arguments: { id: "a" },
    })) as CallToolResult;
    expect(parse(res)).toEqual([
      { name: "API_KEY", default: null, required: true, source: "declared" },
      { name: "REGION", default: "us-east-1", required: false, source: "declared" },
    ]);
  });

  it("unions declared env keys with keys from env resources", async () => {
    const store = fakeStore([{ id: "a", name: "Demo", definition: ENV_DEF }]);
    const rs = fakeResourceStore([
      {
        id: "r1",
        integrationId: "a",
        kind: "env",
        name: ".env.dev",
        // API_KEY collides with a declared var (declared wins); EXTRA is resource-only.
        content: "# creds\nAPI_KEY=from-file\nexport EXTRA=\"hello\"\n",
      },
      // A template resource must be ignored as an env-key source.
      { id: "r2", integrationId: "a", kind: "template", name: "t.tmpl", content: "NOPE=x" },
    ]);
    const client = await connect(config(store, rs));
    const res = (await client.callTool({
      name: "list_env_keys",
      arguments: { id: "a" },
    })) as CallToolResult;
    expect(parse(res)).toEqual([
      { name: "API_KEY", default: null, required: true, source: "declared" },
      { name: "EXTRA", default: "hello", required: false, source: "resource:.env.dev" },
      { name: "REGION", default: "us-east-1", required: false, source: "declared" },
    ]);
  });

  it("registers only list_env_keys when no resourceStore is configured", async () => {
    const store = fakeStore([{ id: "a", name: "Demo", definition: ENV_DEF }]);
    const client = await connect(config(store, undefined));
    const { tools } = await client.listTools();
    const names = tools.map((t) => t.name).sort();
    expect(names).toEqual(["list_env_keys"]);
  });
});
