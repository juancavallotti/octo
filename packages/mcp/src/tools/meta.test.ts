import { describe, expect, it } from "vitest";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { registerMetaTools } from "./meta";
import type { MetaStore, OctoMcpConfig } from "../backend";

/**
 * The flow-meta tools: what an agent leaves behind for the canvas.
 *
 * The assertions worth having here are about refusal and about format. A mock keyed by
 * an address no block answers to looks placed in the file and on the canvas and silently
 * never fires — so it is refused, with the valid addresses in the message. And the file
 * holds a mock's body as JSON *text* while every tool speaks values, so the conversion
 * is pinned in both directions: an agent that has to guess which is which will guess
 * wrong, and a double-escaped body is a mock that matches nothing.
 */

const DEF = [
  "service:",
  "  name: Demo",
  "flows:",
  "  - name: orders",
  "    process:",
  "      - type: log",
  "        name: audit",
  "      - type: log",
  "        name: notify",
  "  - name: reports",
  "    process:",
  "      - type: log",
  "        name: render",
].join("\n");

function fakeStore(definition = DEF): OctoMcpConfig["store"] {
  return {
    list: async () => [{ id: "a", name: "Demo" }],
    get: async (id) => {
      if (id !== "a") throw new Error(`no such integration: ${id}`);
      return { id, name: "Demo", definition };
    },
    create: async () => {
      throw new Error("unused");
    },
    update: async () => {
      throw new Error("unused");
    },
  };
}

/** An in-memory meta file, so a test can read back exactly what was written. */
function fakeMetaStore(seed = ""): MetaStore & { content: string } {
  const held = { content: seed };
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

async function connect(config: OctoMcpConfig): Promise<Client> {
  const server = new McpServer({ name: "octo-test", version: "0.0.0" });
  registerMetaTools(server, config);
  const [clientT, serverT] = InMemoryTransport.createLinkedPair();
  const client = new Client({ name: "test-client", version: "0.0.0" });
  await Promise.all([server.connect(serverT), client.connect(clientT)]);
  return client;
}

function config(metaStore?: MetaStore, definition?: string): OctoMcpConfig {
  return {
    store: fakeStore(definition),
    metaStore,
    validate: () => ({ valid: true, errors: [] }),
    runtimeSchema: {},
  };
}

async function call(
  client: Client,
  name: string,
  args: Record<string, unknown>,
): Promise<CallToolResult> {
  return (await client.callTool({ name, arguments: args })) as CallToolResult;
}

function text(res: CallToolResult): string {
  return (res.content as { type: string; text: string }[])[0].text;
}

function parse(res: CallToolResult): unknown {
  return JSON.parse(text(res));
}

describe("list_block_addresses", () => {
  it("groups every addressable block by its flow", async () => {
    const client = await connect(config());

    expect(parse(await call(client, "list_block_addresses", { id: "a" }))).toEqual([
      { flow: "orders", addresses: ["orders.audit", "orders.notify"] },
      { flow: "reports", addresses: ["reports.render"] },
    ]);
  });

  it("narrows to one flow when asked", async () => {
    const client = await connect(config());

    expect(
      parse(await call(client, "list_block_addresses", { id: "a", flow: "reports" })),
    ).toEqual([{ flow: "reports", addresses: ["reports.render"] }]);
  });

  /**
   * A block whose label is not unique among its siblings has no address at all — the
   * editor will not invent one either, because a key it made up would not survive the
   * reload it exists to survive.
   */
  it("omits a block that no address can name", async () => {
    const ambiguous = [
      "flows:",
      "  - name: orders",
      "    process:",
      "      - type: log",
      "        name: audit",
      "      - type: log",
      "        name: audit",
    ].join("\n");
    const client = await connect(config(undefined, ambiguous));

    expect(parse(await call(client, "list_block_addresses", { id: "a" }))).toEqual([]);
  });

  // It answers a question about the definition, so a host that keeps no meta file still
  // gets it — invoke_flow's `spies` and `breakAt` need it just as much.
  it("is registered without a meta store", async () => {
    const client = await connect(config());

    const names = (await client.listTools()).tools.map((t) => t.name);
    expect(names).toEqual(["list_block_addresses"]);
  });
});

describe("flow meta tools", () => {
  it("saves a test input as JSON text and reads it back", async () => {
    const meta = fakeMetaStore();
    const client = await connect(config(meta));

    const saved = (await call(client, "set_test_input", {
      id: "a",
      flow: "orders",
      name: "a vip order",
      data: { orderId: 42 },
      vars: { tier: "gold" },
    })) as CallToolResult;

    // The file holds JSON text, because the editor edits it in a box...
    expect(meta.content).toContain('\\"orderId\\": 42');
    // ...and the tool answered with the id needed to replace it later.
    expect(parse(saved)).toMatchObject({ name: "a vip order" });
    expect((parse(saved) as { id: string }).id).toBeTruthy();

    const read = parse(await call(client, "get_flow_meta", { id: "a", flow: "orders" }));
    expect(read).toMatchObject({ inputs: [{ name: "a vip order" }] });
  });

  it("replaces an input given its id, rather than adding a second", async () => {
    const meta = fakeMetaStore();
    const client = await connect(config(meta));
    const first = parse(
      await call(client, "set_test_input", { id: "a", flow: "orders", name: "one" }),
    ) as { id: string };

    await call(client, "set_test_input", {
      id: "a",
      flow: "orders",
      name: "renamed",
      inputId: first.id,
    });

    const read = parse(
      await call(client, "get_flow_meta", { id: "a", flow: "orders" }),
    ) as { inputs: { name: string }[] };
    expect(read.inputs).toEqual([{ id: first.id, name: "renamed" }]);
  });

  it("deletes a test input", async () => {
    const meta = fakeMetaStore();
    const client = await connect(config(meta));
    const saved = parse(
      await call(client, "set_test_input", { id: "a", flow: "orders", name: "one" }),
    ) as { id: string };

    const left = parse(
      await call(client, "delete_test_input", {
        id: "a",
        flow: "orders",
        inputId: saved.id,
      }),
    );

    expect(left).toEqual([]);
  });

  /**
   * The conversion the two model halves force. The file holds a mock's body as JSON
   * text; every tool speaks values. An agent asked for "a string of JSON" would
   * double-escape it sooner or later, and a double-escaped body matches nothing.
   */
  it("stores a mock's body as text and returns it as a value", async () => {
    const meta = fakeMetaStore();
    const client = await connect(config(meta));

    const res = await call(client, "set_mock", {
      id: "a",
      address: "orders.audit",
      cases: [{ when: 'body.tier == "vip"', body: { charged: true } }],
      default: { drop: true },
    });

    expect(meta.content).toContain('\\"charged\\": true');
    expect(parse(res)).toEqual({
      address: "orders.audit",
      enabled: true,
      cases: [{ when: 'body.tier == "vip"', body: { charged: true } }],
      default: { drop: true },
    });
  });

  it("files a mock under the flow its address names", async () => {
    const meta = fakeMetaStore();
    const client = await connect(config(meta));

    await call(client, "set_mock", {
      id: "a",
      address: "reports.render",
      default: { body: { ok: true } },
    });

    const orders = parse(
      await call(client, "get_flow_meta", { id: "a", flow: "orders" }),
    ) as { mocks: unknown[] };
    const reports = parse(
      await call(client, "get_flow_meta", { id: "a", flow: "reports" }),
    ) as { mocks: { address: string }[] };
    expect(orders.mocks).toEqual([]);
    expect(reports.mocks[0].address).toBe("reports.render");
  });

  /**
   * The refusal that matters. Writing the mock anyway would leave one that looks placed
   * and never fires; renaming the block to make the address work would be a tool editing
   * the definition it was asked to describe.
   */
  it("refuses an address no block answers to, and says which do", async () => {
    const meta = fakeMetaStore();
    const client = await connect(config(meta));

    const res = await call(client, "set_mock", {
      id: "a",
      address: "orders.charge",
      default: { drop: true },
    });

    expect(res.isError).toBe(true);
    expect(text(res)).toContain("orders.audit, orders.notify");
    expect(text(res)).toContain("update_flow");
    expect(meta.content).toBe("");
  });

  it("refuses a case that sets two outcomes", async () => {
    const meta = fakeMetaStore();
    const client = await connect(config(meta));

    const res = await call(client, "set_mock", {
      id: "a",
      address: "orders.audit",
      cases: [{ when: "true", body: { ok: true }, drop: true }],
    });

    expect(res.isError).toBe(true);
    expect(text(res)).toContain("exactly one");
    expect(meta.content).toBe("");
  });

  it("refuses a default that carries a when", async () => {
    const client = await connect(config(fakeMetaStore()));

    const res = await call(client, "set_mock", {
      id: "a",
      address: "orders.audit",
      default: { when: "true", drop: true },
    });

    expect(res.isError).toBe(true);
    expect(text(res)).toContain("must not carry a `when`");
  });

  // A mock that says nothing replaces the block with nothing, which fails every message
  // that reaches it — never what anyone means.
  it("refuses a mock with neither cases nor a default", async () => {
    const client = await connect(config(fakeMetaStore()));

    const res = await call(client, "set_mock", { id: "a", address: "orders.audit" });

    expect(res.isError).toBe(true);
    expect(text(res)).toContain("must say what the block does");
  });

  it("replaces the mock on an address rather than adding a second", async () => {
    const meta = fakeMetaStore();
    const client = await connect(config(meta));

    await call(client, "set_mock", { id: "a", address: "orders.audit", default: { drop: true } });
    await call(client, "set_mock", {
      id: "a",
      address: "orders.audit",
      default: { body: { ok: true } },
    });

    const read = parse(
      await call(client, "get_flow_meta", { id: "a", flow: "orders" }),
    ) as { mocks: { default: unknown }[] };
    expect(read.mocks).toHaveLength(1);
    expect(read.mocks[0].default).toEqual({ body: { ok: true } });
  });

  it("deletes a mock", async () => {
    const client = await connect(config(fakeMetaStore()));
    await call(client, "set_mock", { id: "a", address: "orders.audit", default: { drop: true } });

    const left = parse(
      await call(client, "delete_mock", { id: "a", address: "orders.audit" }),
    );

    expect(left).toEqual([]);
  });

  it("watches a block once, however often it is asked", async () => {
    const client = await connect(config(fakeMetaStore()));

    await call(client, "set_spy", { id: "a", address: "orders.audit" });
    const spies = parse(await call(client, "set_spy", { id: "a", address: "orders.audit" }));

    expect(spies).toEqual(["orders.audit"]);
  });

  it("refuses to watch an address no block answers to", async () => {
    const meta = fakeMetaStore();
    const client = await connect(config(meta));

    const res = await call(client, "set_spy", { id: "a", address: "orders.nope" });

    expect(res.isError).toBe(true);
    expect(meta.content).toBe("");
  });

  it("stops watching a block", async () => {
    const client = await connect(config(fakeMetaStore()));
    await call(client, "set_spy", { id: "a", address: "orders.audit" });

    expect(
      parse(await call(client, "delete_spy", { id: "a", address: "orders.audit" })),
    ).toEqual([]);
  });

  /**
   * The editor writes this file too. Re-reading before every change is what keeps an
   * agent's write from reverting whatever the user did on the canvas in between.
   */
  it("keeps what another writer put in the file", async () => {
    const meta = fakeMetaStore();
    const client = await connect(config(meta));
    await call(client, "set_spy", { id: "a", address: "orders.audit" });

    // The canvas saves an input while the agent is thinking.
    meta.load = async () =>
      JSON.stringify({
        version: 1,
        resources: {
          a: {
            flows: {
              orders: { inputs: [{ id: "i1", name: "from the canvas" }], spies: ["orders.audit"] },
            },
          },
        },
      });

    await call(client, "set_spy", { id: "a", address: "orders.notify" });

    expect(meta.content).toContain("from the canvas");
    expect(meta.content).toContain("orders.notify");
  });

  it("is not registered on a host that keeps no meta file", async () => {
    const client = await connect(config(undefined));

    const names = (await client.listTools()).tools.map((t) => t.name);
    expect(names).not.toContain("set_mock");
  });
});
