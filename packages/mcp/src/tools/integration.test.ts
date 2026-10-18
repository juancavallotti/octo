import { describe, expect, it } from "vitest";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { registerIntegrationTools } from "./integration";
import type { IntegrationRecord, MetaStore, OctoMcpConfig, StoredFile } from "../backend";

/** One config file, the shape a single-file integration takes. */
const one = (content: string): StoredFile[] => [{ path: "octo.yaml", content }];

/** A trivial in-memory IntegrationStore for exercising the tools end to end. */
function fakeStore(seed: IntegrationRecord[] = []) {
  const rows = new Map(seed.map((r) => [r.id, { ...r }]));
  let seq = rows.size;
  return {
    rows,
    list: async () =>
      [...rows.values()].map(({ id, name }) => ({ id, name })),
    get: async (id: string) => {
      const r = rows.get(id);
      if (!r) throw new Error(`no such integration: ${id}`);
      return { ...r };
    },
    create: async (name: string, files: StoredFile[]) => {
      const id = `id-${++seq}`;
      const r = { id, name, files };
      rows.set(id, r);
      return { ...r };
    },
    update: async (id: string, name: string | undefined, files: StoredFile[]) => {
      const r = rows.get(id);
      if (!r) throw new Error(`no such integration: ${id}`);
      const next = { id, name: name ?? r.name, files };
      rows.set(id, next);
      return { ...next };
    },
  };
}

/** Stand up a linked client/server with the integration tools registered. */
async function connect(config: OctoMcpConfig): Promise<Client> {
  const server = new McpServer({ name: "octo-test", version: "0.0.0" });
  registerIntegrationTools(server, config);
  const [clientT, serverT] = InMemoryTransport.createLinkedPair();
  const client = new Client({ name: "test-client", version: "0.0.0" });
  await Promise.all([server.connect(serverT), client.connect(clientT)]);
  return client;
}

/** The content of one stored file. */
function content(store: ReturnType<typeof fakeStore>, id: string, path = "octo.yaml"): string | undefined {
  return store.rows.get(id)?.files.find((f) => f.path === path)?.content;
}

function baseConfig(store: OctoMcpConfig["store"]): OctoMcpConfig {
  return { store, validate: () => ({ valid: true, errors: [] }), runtimeSchema: {} };
}

/** An in-memory `.octo/editor-meta.json`, seeded with one integration's flows. */
function metaFile(flows: Record<string, unknown>): MetaStore & { content: string } {
  const held = {
    content: JSON.stringify({ version: 1, resources: { a: { flows } } }, null, 2),
  };
  return {
    get content() {
      return held.content;
    },
    load: async () => held.content,
    save: async (_id, content) => {
      held.content = content;
    },
  };
}

/** Parse the JSON a tool returned in its first text content block. */
function parse(res: CallToolResult): unknown {
  const block = (res.content as { type: string; text: string }[])[0];
  return JSON.parse(block.text);
}

describe("integration tools", () => {
  it("lists integrations as { id, name }", async () => {
    const store = fakeStore([
      { id: "a", name: "Alpha", files: one("x") },
      { id: "b", name: "Beta", files: one("y") },
    ]);
    const client = await connect(baseConfig(store));
    const res = (await client.callTool({
      name: "list_integrations",
      arguments: {},
    })) as CallToolResult;
    expect(parse(res)).toEqual([
      { id: "a", name: "Alpha" },
      { id: "b", name: "Beta" },
    ]);
  });

  it("opens an integration by id", async () => {
    const store = fakeStore([{ id: "a", name: "Alpha", files: one("the-yaml") }]);
    const client = await connect(baseConfig(store));
    const res = (await client.callTool({
      name: "open_integration",
      arguments: { id: "a" },
    })) as CallToolResult;
    expect(parse(res)).toEqual({ id: "a", name: "Alpha", files: one("the-yaml") });
  });

  it("returns an error result when opening a missing integration", async () => {
    const store = fakeStore();
    const client = await connect(baseConfig(store));
    const res = (await client.callTool({
      name: "open_integration",
      arguments: { id: "nope" },
    })) as CallToolResult;
    expect(res.isError).toBe(true);
    expect((res.content as { text: string }[])[0].text).toContain("nope");
  });

  it("creates an integration", async () => {
    const store = fakeStore();
    const client = await connect(baseConfig(store));
    const res = (await client.callTool({
      name: "create_integration",
      arguments: { name: "New", files: one("service:\n  name: New") },
    })) as CallToolResult;
    const created = parse(res) as IntegrationRecord;
    expect(created.name).toBe("New");
    expect(content(store, created.id)).toBe("service:\n  name: New");
  });

  it("validates draft files without saving, returning descriptive errors", async () => {
    const store = fakeStore();
    const cfg: OctoMcpConfig = {
      store,
      runtimeSchema: {},
      validate: (files) =>
        files.some((f) => f.content.includes("flows"))
          ? { valid: true, errors: [] }
          : { valid: false, errors: ['Flow "x": source is incomplete.'] },
    };
    const client = await connect(cfg);

    const bad = (await client.callTool({
      name: "validate_project",
      arguments: { files: one("service:\n  name: x") },
    })) as CallToolResult;
    expect(parse(bad)).toEqual({
      valid: false,
      errors: ['Flow "x": source is incomplete.'],
    });

    const good = (await client.callTool({
      name: "validate_project",
      arguments: { files: [...one("service:\n  name: x"), { path: "orders.yaml", content: "flows: []" }] },
    })) as CallToolResult;
    expect(parse(good)).toEqual({ valid: true, errors: [] });

    // Pure check: nothing was written to the store.
    expect(store.rows.size).toBe(0);
  });

  it("lists flows with their source type, null for sourceless flows", async () => {
    const definition = [
      "service:",
      "  name: Demo",
      "flows:",
      "  - name: greet",
      "    source:",
      "      connector: ticker",
      "      type: cron",
      "    process: []",
      "  - name: helper",
      "    process: []",
    ].join("\n");
    const store = fakeStore([{ id: "a", name: "Demo", files: one(definition) }]);
    const client = await connect(baseConfig(store));
    const res = (await client.callTool({
      name: "list_flows",
      arguments: { id: "a" },
    })) as CallToolResult;
    expect(parse(res)).toEqual([
      { name: "greet", source: "cron", file: "octo.yaml" },
      { name: "helper", source: null, file: "octo.yaml" },
    ]);
  });

  it("returns [] when the definition declares no flows", async () => {
    const store = fakeStore([
      { id: "a", name: "Empty", files: one("service:\n  name: Empty\n") },
    ]);
    const client = await connect(baseConfig(store));
    const res = (await client.callTool({
      name: "list_flows",
      arguments: { id: "a" },
    })) as CallToolResult;
    expect(parse(res)).toEqual([]);
  });

  it("replaces an integration's files and name", async () => {
    const store = fakeStore([{ id: "a", name: "Old", files: one("v1") }]);
    const client = await connect(baseConfig(store));
    const res = (await client.callTool({
      name: "update_integration",
      arguments: { id: "a", name: "Renamed", files: one("v2") },
    })) as CallToolResult;
    expect(parse(res)).toEqual({ id: "a", name: "Renamed", files: one("v2") });
    expect(store.rows.get("a")).toMatchObject({ name: "Renamed", files: one("v2") });
  });

  describe("flow-level editing", () => {
    /** A definition carrying a comment and a second flow, both of which must survive. */
    const DEF = `service:
  name: Orders

flows:
  # Charges the card.
  - name: charge
    process:
      - type: rest
        name: call-stripe

  - name: audit
    process:
      - type: log
        name: write-it
`;

    const seeded = () => fakeStore([{ id: "a", name: "Orders", files: one(DEF) }]);

    /** The text body of a tool result (get_flow returns YAML, not JSON). */
    const text = (res: CallToolResult) => (res.content as { text: string }[])[0].text;

    it("get_flow returns one flow's YAML with its comments", async () => {
      const client = await connect(baseConfig(seeded()));
      const res = (await client.callTool({
        name: "get_flow",
        arguments: { id: "a", flow: "charge" },
      })) as CallToolResult;
      expect(res.isError).toBeFalsy();
      expect(text(res)).toContain("call-stripe");
      expect(text(res)).not.toContain("audit");
    });

    // The point of the whole feature: editing one flow must not disturb the others.
    it("update_flow replaces one flow and leaves the rest of the file intact", async () => {
      const store = seeded();
      const client = await connect(baseConfig(store));
      const res = (await client.callTool({
        name: "update_flow",
        arguments: {
          id: "a",
          flow: "charge",
          definition: "name: charge\nprocess:\n  - type: log\n    name: pretend\n",
        },
      })) as CallToolResult;
      expect(res.isError).toBeFalsy();

      const saved = content(store, "a")!;
      expect(saved).toContain("# Charges the card.");
      expect(saved).toContain("name: write-it"); // the other flow, untouched
      expect(saved).toContain("name: pretend");
      expect(saved).not.toContain("call-stripe");
    });

    it("add_flow appends a flow, and refuses a name already taken", async () => {
      const store = seeded();
      const client = await connect(baseConfig(store));

      const ok = (await client.callTool({
        name: "add_flow",
        arguments: { id: "a", definition: "name: refund\nprocess: []\n" },
      })) as CallToolResult;
      expect(ok.isError).toBeFalsy();
      expect(content(store, "a")!).toContain("name: refund");

      const dup = (await client.callTool({
        name: "add_flow",
        arguments: { id: "a", definition: "name: audit\nprocess: []\n" },
      })) as CallToolResult;
      expect(dup.isError).toBe(true);
      expect(text(dup)).toContain("already exists");
    });

    it("delete_flow removes just that flow", async () => {
      const store = seeded();
      const client = await connect(baseConfig(store));
      const res = (await client.callTool({
        name: "delete_flow",
        arguments: { id: "a", flow: "charge" },
      })) as CallToolResult;
      expect(res.isError).toBeFalsy();

      const saved = content(store, "a")!;
      expect(saved).not.toContain("name: charge");
      expect(saved).toContain("name: audit");
    });

    // Every real host validates asynchronously: the catalogue a definition is checked
    // against is generated by the `octo` binary, and reaching it is a probe. A host
    // whose validate() returns a promise must be awaited — not truthiness-tested,
    // which a promise always passes, quietly saving the very thing we refused.
    it("awaits an async validate before saving", async () => {
      const store = seeded();
      const config: OctoMcpConfig = {
        ...baseConfig(store),
        validate: async () => ({ valid: false, errors: ["unknown block type"] }),
      };
      const client = await connect(config);

      const res = (await client.callTool({
        name: "update_flow",
        arguments: {
          id: "a",
          flow: "charge",
          definition: "name: charge\nprocess:\n  - type: nope\n",
        },
      })) as CallToolResult;
      expect(res.isError).toBe(true);
      expect(text(res)).toContain("unknown block type");
      expect(content(store, "a")!).toBe(DEF); // untouched
    });

    // These tools exist so an agent can edit a flow without holding the whole file in
    // its head — which means it also cannot see what it just broke. Validating the
    // spliced whole, and refusing to save, is what makes that safe.
    it("refuses a splice that would leave the integration invalid, saving nothing", async () => {
      const store = seeded();
      const config: OctoMcpConfig = {
        ...baseConfig(store),
        validate: () => ({ valid: false, errors: ['block "nope" is not a known type'] }),
      };
      const client = await connect(config);

      const res = (await client.callTool({
        name: "update_flow",
        arguments: {
          id: "a",
          flow: "charge",
          definition: "name: charge\nprocess:\n  - type: nope\n",
        },
      })) as CallToolResult;
      expect(res.isError).toBe(true);
      expect(text(res)).toContain("nothing was saved");
      expect(content(store, "a")!).toBe(DEF); // untouched
    });

    /**
     * A block address opens with its flow's name, so a rename orphans the editor's
     * bookkeeping AND every address inside it. Left behind, the mocks stay listed in the
     * file and drawn on the canvas and never fire again — worse than losing them, because
     * nothing says they are gone.
     */
    it("carries the editor's inputs, mocks and spies across a rename", async () => {
      const store = seeded();
      const meta = metaFile({
        charge: {
          inputs: [{ id: "i1", name: "a vip order" }],
          mocks: [{ address: "charge.call-stripe", enabled: true, cases: [], default: { drop: true } }],
          spies: ["charge.call-stripe"],
        },
      });
      const client = await connect({ ...baseConfig(store), metaStore: meta });

      const res = (await client.callTool({
        name: "update_flow",
        arguments: {
          id: "a",
          flow: "charge",
          definition: "name: settle\nprocess:\n  - type: rest\n    name: call-stripe\n",
        },
      })) as CallToolResult;
      expect(res.isError).toBeFalsy();

      const written = JSON.parse(meta.content).resources.a.flows;
      expect(written.charge).toBeUndefined();
      expect(written.settle.inputs[0].name).toBe("a vip order");
      // Re-rooted, not merely moved: an address still naming "charge" would fire for
      // nothing.
      expect(written.settle.mocks[0].address).toBe("settle.call-stripe");
      expect(written.settle.spies).toEqual(["settle.call-stripe"]);
    });

    it("leaves the meta alone when the flow was edited but not renamed", async () => {
      const store = seeded();
      const meta = metaFile({ charge: { inputs: [{ id: "i1", name: "a vip order" }] } });
      const before = meta.content;
      const client = await connect({ ...baseConfig(store), metaStore: meta });

      await client.callTool({
        name: "update_flow",
        arguments: {
          id: "a",
          flow: "charge",
          definition: "name: charge\nprocess:\n  - type: log\n    name: pretend\n",
        },
      });

      expect(meta.content).toBe(before);
    });

    /**
     * The definition is already saved by the time the meta is touched, so a store that
     * fails must not report the rename as failed — an agent that undid it would leave the
     * user worse off than the stale addresses do.
     */
    it("reports a meta store that failed without failing the rename", async () => {
      const store = seeded();
      const meta = metaFile({ charge: { inputs: [] } });
      meta.save = async () => {
        throw new Error("the disk is full");
      };
      const client = await connect({ ...baseConfig(store), metaStore: meta });

      const res = (await client.callTool({
        name: "update_flow",
        arguments: { id: "a", flow: "charge", definition: "name: settle\nprocess: []\n" },
      })) as CallToolResult;

      expect(res.isError).toBeFalsy();
      expect(content(store, "a")!).toContain("name: settle");
      const blocks = res.content as { text: string }[];
      expect(blocks[blocks.length - 1].text).toContain("the disk is full");
      expect(blocks[blocks.length - 1].text).toContain("will not fire");
    });

    it("errors when the flow does not exist, naming the ones that do", async () => {
      const client = await connect(baseConfig(seeded()));
      const res = (await client.callTool({
        name: "update_flow",
        arguments: { id: "a", flow: "refund", definition: "name: refund\nprocess: []\n" },
      })) as CallToolResult;
      expect(res.isError).toBe(true);
      expect(text(res)).toContain('no flow named "refund"');
      expect(text(res)).toContain('"charge", "audit"');
    });
  });

  describe("an integration of several files", () => {
    const OCTO = "service:\n  name: Shop\nconnectors:\n  - name: api\n    type: http\n";
    const ORDERS = "# Orders.\nflows:\n  - name: orders\n    process: []\n";
    const seeded = () =>
      fakeStore([
        {
          id: "a",
          name: "Shop",
          files: [
            { path: "octo.yaml", content: OCTO },
            { path: "orders.yaml", content: ORDERS },
          ],
        },
      ]);
    const text = (res: CallToolResult) => (res.content as { text: string }[])[0].text;

    it("lists its files and the flows each declares", async () => {
      const client = await connect(baseConfig(seeded()));
      const res = (await client.callTool({ name: "list_files", arguments: { id: "a" } })) as CallToolResult;
      expect(parse(res)).toEqual([
        { path: "octo.yaml", flows: [] },
        { path: "orders.yaml", flows: ["orders"] },
      ]);
    });

    it("edits a flow in the file that declares it, leaving the others alone", async () => {
      const store = seeded();
      const client = await connect(baseConfig(store));
      const res = (await client.callTool({
        name: "update_flow",
        arguments: { id: "a", flow: "orders", definition: "name: orders\nprocess:\n  - type: log\n" },
      })) as CallToolResult;
      expect(res.isError).toBeFalsy();
      expect(content(store, "a", "orders.yaml")).toContain("# Orders.");
      expect(content(store, "a", "orders.yaml")).toContain("type: log");
      expect(content(store, "a", "octo.yaml")).toBe(OCTO);
    });

    it("asks which file a new flow goes in, and creates a file it names", async () => {
      const store = seeded();
      const client = await connect(baseConfig(store));
      const ask = (await client.callTool({
        name: "add_flow",
        arguments: { id: "a", definition: "name: billing\nprocess: []\n" },
      })) as CallToolResult;
      expect(ask.isError).toBe(true);
      expect(text(ask)).toContain("octo.yaml, orders.yaml");

      const ok = (await client.callTool({
        name: "add_flow",
        arguments: { id: "a", definition: "name: billing\nprocess: []\n", file: "billing.yaml" },
      })) as CallToolResult;
      expect(ok.isError).toBeFalsy();
      expect(content(store, "a", "billing.yaml")).toContain("name: billing");
    });

    it("refuses a flow whose name another file already declares", async () => {
      const client = await connect(baseConfig(seeded()));
      const res = (await client.callTool({
        name: "add_flow",
        arguments: { id: "a", definition: "name: orders\nprocess: []\n", file: "octo.yaml" },
      })) as CallToolResult;
      expect(res.isError).toBe(true);
      expect(text(res)).toContain("already exists in orders.yaml");
    });

    it("validates every file together before writing one", async () => {
      const store = seeded();
      const seen: string[][] = [];
      const client = await connect({
        ...baseConfig(store),
        validate: (files) => {
          seen.push(files.map((f) => f.path));
          return { valid: false, errors: ['connector "api" is not configured'] };
        },
      });
      const res = (await client.callTool({
        name: "write_file",
        arguments: { id: "a", path: "octo.yaml", content: "service:\n  name: Shop\n" },
      })) as CallToolResult;
      expect(res.isError).toBe(true);
      expect(seen).toEqual([["octo.yaml", "orders.yaml"]]);
      expect(content(store, "a", "octo.yaml")).toBe(OCTO);
    });

    it("refuses a name the runtime would not load as config", async () => {
      const client = await connect(baseConfig(seeded()));
      const res = (await client.callTool({
        name: "write_file",
        arguments: { id: "a", path: "flows/orders.yaml", content: "flows: []\n" },
      })) as CallToolResult;
      expect(res.isError).toBe(true);
      expect(text(res)).toContain("not config file names: flows/orders.yaml");
    });

    it("deletes a file, but never the last one", async () => {
      const store = seeded();
      const client = await connect(baseConfig(store));
      await client.callTool({ name: "delete_file", arguments: { id: "a", path: "orders.yaml" } });
      expect(store.rows.get("a")!.files.map((f) => f.path)).toEqual(["octo.yaml"]);
      const last = (await client.callTool({
        name: "delete_file",
        arguments: { id: "a", path: "octo.yaml" },
      })) as CallToolResult;
      expect(last.isError).toBe(true);
    });
  });
});
