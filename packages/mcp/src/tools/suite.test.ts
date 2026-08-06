import { describe, expect, it } from "vitest";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import type { TestRunArgs, TestRunOutcome } from "@octo/run-host";
import { registerSuiteTools } from "./suite";
import type { OctoMcpConfig, SuiteStore } from "../backend";
import type { RunHostPort } from "../run-host";

/**
 * The test-authoring tools.
 *
 * Two properties carry the weight. A suite that would not load is refused before
 * anything is written — dolphin takes the whole file down over one bad key, so a partial
 * save would cost the agent every case in it, not just the one it got wrong. And
 * `run_tests` reports a FAILING TEST as a successful run: dolphin exits non-zero for a
 * failed case, and folding that into an error would hide the failure the caller is trying
 * to read behind "the run failed".
 */

const DEF = [
  "service:",
  "  name: Demo",
  "flows:",
  "  - name: orders",
  "    process:",
  "      - type: log",
  "        name: audit",
].join("\n");

const SUITE = [
  "# what orders should do",
  "flow: orders",
  "cases:",
  "  - name: it runs",
  "    expect:",
  "      body: {ok: true}",
  "",
].join("\n");

function fakeStore(): OctoMcpConfig["store"] {
  return {
    list: async () => [{ id: "a", name: "Demo" }],
    get: async (id) => {
      if (id !== "a") throw new Error(`no such integration: ${id}`);
      return { id, name: "Demo", definition: DEF };
    },
    create: async () => {
      throw new Error("unused");
    },
    update: async () => {
      throw new Error("unused");
    },
  };
}

function fakeSuiteStore(seed: { flow: string; content: string }[] = []) {
  const rows = new Map(seed.map((s) => [s.flow, s.content]));
  const store: SuiteStore & { rows: Map<string, string> } = {
    rows,
    list: async () => [...rows].map(([flow, content]) => ({ flow, content })),
    save: async (_id, flow, content) => {
      rows.set(flow, content);
    },
    remove: async (_id, flow) => {
      rows.delete(flow);
    },
  };
  return store;
}

/** A tally with the given counts filled in over zeroes. */
function totals(over: Partial<TestRunOutcome["totals"]> = {}) {
  return {
    cases: 0,
    passed: 0,
    failed: 0,
    errored: 0,
    skipped: 0,
    notRun: 0,
    elapsedMs: 0,
    ...over,
  };
}

/** A run host whose `test` returns whatever the test wants, recording what it was asked. */
function fakeRunHost(outcome?: Partial<TestRunOutcome>) {
  const seen: TestRunArgs[] = [];
  const host = {
    seen,
    binaries: () => ({ available: true, version: null }),
    status: () => ({
      running: false,
      exposable: false,
      port: null,
      testUrl: null,
    }),
    start: async () => host.status(),
    stop: async () => host.status(),
    invoke: async () => ({
      ok: true,
      exitCode: 0,
      timedOut: false,
      dropped: false,
      output: "",
      logs: [],
    }),
    evalCel: async () => ({ ok: true, result: null, logs: [] }),
    test: async (_ns: string, args: TestRunArgs) => {
      seen.push(args);
      return {
        ok: true,
        exitCode: 0,
        timedOut: false,
        totals: totals({ cases: 1, passed: 1 }),
        suites: [],
        logs: [],
        ...outcome,
      } as TestRunOutcome;
    },
    snapshot: () => [],
    newNamespace: () => "ns-1",
  };
  return host as RunHostPort & { seen: TestRunArgs[] };
}

async function connect(
  suiteStore: SuiteStore | undefined,
  runHost: RunHostPort = fakeRunHost(),
): Promise<Client> {
  const server = new McpServer({ name: "octo-test", version: "0.0.0" });
  registerSuiteTools(
    server,
    {
      store: fakeStore(),
      suiteStore,
      validate: () => ({ valid: true, errors: [] }),
      runtimeSchema: {},
    },
    runHost,
    () => "ns-1",
  );
  const [clientT, serverT] = InMemoryTransport.createLinkedPair();
  const client = new Client({ name: "test-client", version: "0.0.0" });
  await Promise.all([server.connect(serverT), client.connect(clientT)]);
  return client;
}

async function call(
  client: Client,
  name: string,
  args: Record<string, unknown>,
): Promise<CallToolResult> {
  return (await client.callTool({ name, arguments: args })) as CallToolResult;
}

const blocks = (res: CallToolResult) => res.content as { type: string; text: string }[];
const text = (res: CallToolResult) => blocks(res)[0].text;
const parse = (res: CallToolResult) => JSON.parse(text(res));

describe("test-suite tools", () => {
  it("lists the suites with their case names", async () => {
    const client = await connect(fakeSuiteStore([{ flow: "orders", content: SUITE }]));

    expect(parse(await call(client, "list_test_suites", { id: "a" }))).toEqual([
      { flow: "orders", file: "orders_test.yaml", cases: ["it runs"] },
    ]);
  });

  it("returns a suite's YAML exactly as stored, comments and all", async () => {
    const client = await connect(fakeSuiteStore([{ flow: "orders", content: SUITE }]));

    expect(text(await call(client, "get_test_suite", { id: "a", flow: "orders" }))).toBe(
      SUITE,
    );
  });

  it("writes a whole suite byte for byte, under the flow the file declares", async () => {
    const suites = fakeSuiteStore();
    const client = await connect(suites);

    const res = await call(client, "set_test_suite", { id: "a", content: SUITE });

    expect(res.isError).toBeFalsy();
    expect(suites.rows.get("orders")).toBe(SUITE);
  });

  /**
   * dolphin decodes with unknown keys rejected, because a typo'd key that was silently
   * ignored would look exactly like a test that passes: a misspelled `spys:` watches
   * nothing and goes green. So the editor is just as strict, and so is this.
   */
  it("refuses a suite dolphin would reject, saving nothing", async () => {
    const suites = fakeSuiteStore();
    const client = await connect(suites);

    const res = await call(client, "set_test_suite", {
      id: "a",
      content: "flow: orders\ncases:\n  - name: it runs\n    spys: {}\n",
    });

    expect(res.isError).toBe(true);
    expect(text(res)).toContain("nothing was saved");
    expect(suites.rows.size).toBe(0);
  });

  it("adds a case to a flow that had no suite", async () => {
    const suites = fakeSuiteStore();
    const client = await connect(suites);

    const res = await call(client, "set_test_case", {
      id: "a",
      flow: "orders",
      case: {
        name: "it charges",
        input: { data: { orderId: 42 } },
        expect: { body: { charged: true }, that: ["body.charged"] },
        spies: { "orders.audit": { count: 0 } },
      },
    });

    expect(res.isError).toBeFalsy();
    const written = suites.rows.get("orders")!;
    expect(written).toContain("flow: orders");
    expect(written).toContain("name: it charges");
    expect(written).toContain("orderId: 42");
    // count: 0 is an assertion — the block never ran — not an absence to be dropped.
    expect(written).toContain("count: 0");
  });

  it("replaces the case of the same name rather than adding a second", async () => {
    const suites = fakeSuiteStore([{ flow: "orders", content: SUITE }]);
    const client = await connect(suites);

    const res = await call(client, "set_test_case", {
      id: "a",
      flow: "orders",
      case: { name: "it runs", expect: { dropped: true } },
    });

    expect(parse(res).cases).toEqual(["it runs"]);
    expect(suites.rows.get("orders")).toContain("dropped: true");
    expect(suites.rows.get("orders")).not.toContain("ok: true");
  });

  /**
   * set_test_case rewrites the file from its parsed form, so the comments go. Saying so
   * is the difference between a tool that costs the user their notes and one that tells
   * them where to write them back.
   */
  it("warns that a commented suite lost its comments", async () => {
    const client = await connect(fakeSuiteStore([{ flow: "orders", content: SUITE }]));

    const res = await call(client, "set_test_case", {
      id: "a",
      flow: "orders",
      case: { name: "another" },
    });

    expect(res.isError).toBeFalsy();
    expect(blocks(res).at(-1)!.text).toContain("comments were dropped");
  });

  it("refuses to rewrite a suite it could not fully read", async () => {
    const suites = fakeSuiteStore([
      { flow: "orders", content: "flow: orders\ncases:\n  - name: x\n    spys: {}\n" },
    ]);
    const client = await connect(suites);
    const before = suites.rows.get("orders");

    const res = await call(client, "set_test_case", {
      id: "a",
      flow: "orders",
      case: { name: "another" },
    });

    expect(res.isError).toBe(true);
    expect(suites.rows.get("orders")).toBe(before);
  });

  it("deletes a case, and says which exist when the name is wrong", async () => {
    const suites = fakeSuiteStore([{ flow: "orders", content: SUITE }]);
    const client = await connect(suites);

    const missing = await call(client, "delete_test_case", {
      id: "a",
      flow: "orders",
      name: "nope",
    });
    expect(missing.isError).toBe(true);
    expect(text(missing)).toContain("it runs");

    // A suite with no cases at all is one dolphin refuses, so removing the last case
    // takes the same refusal as writing an empty one.
    await call(client, "set_test_case", { id: "a", flow: "orders", case: { name: "second" } });
    const res = await call(client, "delete_test_case", {
      id: "a",
      flow: "orders",
      name: "it runs",
    });
    expect(parse(res).cases).toEqual(["second"]);
  });
});

describe("run_tests", () => {
  it("runs the stored suites against the integration's definition", async () => {
    const host = fakeRunHost();
    const client = await connect(fakeSuiteStore([{ flow: "orders", content: SUITE }]), host);

    const res = await call(client, "run_tests", { id: "a" });

    expect(res.isError).toBeFalsy();
    expect(host.seen[0].yaml).toBe(DEF);
    expect(host.seen[0].suites).toEqual([{ name: "orders", content: SUITE }]);
    expect(parse(res).totals.passed).toBe(1);
  });

  it("runs one flow's suite when asked", async () => {
    const host = fakeRunHost();
    const client = await connect(
      fakeSuiteStore([
        { flow: "orders", content: SUITE },
        { flow: "reports", content: "flow: reports\ncases:\n  - name: it runs\n" },
      ]),
      host,
    );

    await call(client, "run_tests", { id: "a", flow: "reports" });

    expect(host.seen[0].suites.map((s) => s.name)).toEqual(["reports"]);
  });

  /**
   * The exit-code trap. dolphin exits 1 for a failed case and 2 for an errored one, and
   * both are ordinary outcomes of running tests. Reporting them as tool errors would tell
   * the caller "the run failed" and hide the failing case they came to read.
   */
  it("reports a failing test as a successful run", async () => {
    const host = fakeRunHost({
      exitCode: 1,
      totals: totals({ cases: 2, passed: 1, failed: 1 }),
      suites: [
        {
          name: "orders",
          flow: "orders",
          cases: [
            { name: "it runs", status: "passed", elapsedMs: 3 },
            {
              name: "it charges",
              status: "failed",
              elapsedMs: 4,
              summary: "body did not match",
              failures: [{ summary: "body did not match", detail: "want ok:true\ngot ok:false" }],
              outcome: { result: { body: { ok: false } } },
            },
          ],
        },
      ],
    });
    const client = await connect(fakeSuiteStore([{ flow: "orders", content: SUITE }]), host);

    const res = await call(client, "run_tests", { id: "a" });

    expect(res.isError).toBeFalsy();
    const report = parse(res);
    expect(report.ok).toBe(true);
    expect(report.totals.failed).toBe(1);
    // A passing case is reported plainly; a failing one carries what to act on — the
    // detail dolphin printed, and the message the flow actually produced.
    expect(report.suites[0].cases[0]).toEqual({
      name: "it runs",
      status: "passed",
      elapsedMs: 3,
    });
    expect(report.suites[0].cases[1].failures[0].detail).toContain("got ok:false");
    expect(report.suites[0].cases[1].outcome.result.body.ok).toBe(false);
  });

  // A run that could not be made at all IS an error: no dolphin, an unreadable report.
  it("errors when the run itself could not be made", async () => {
    const host = fakeRunHost({ ok: false, error: "DOLPHIN_BIN_PATH is not set" });
    const client = await connect(fakeSuiteStore([{ flow: "orders", content: SUITE }]), host);

    const res = await call(client, "run_tests", { id: "a" });

    expect(res.isError).toBe(true);
    expect(text(res)).toContain("DOLPHIN_BIN_PATH");
  });

  it("says so when there is nothing to run", async () => {
    const client = await connect(fakeSuiteStore());

    const res = await call(client, "run_tests", { id: "a" });

    expect(res.isError).toBe(true);
    expect(text(res)).toContain("no test suites yet");
  });

  it("registers nothing on a host that keeps no suites", async () => {
    expect((await (await connect(fakeSuiteStore())).listTools()).tools.map((t) => t.name))
      .toEqual([
        "list_test_suites",
        "get_test_suite",
        "set_test_suite",
        "set_test_case",
        "delete_test_case",
        "run_tests",
      ]);

    // With no suite store, not one of them is registered — and a server with no tools at
    // all does not advertise the capability, so even asking is a protocol error.
    const bare = await connect(undefined);
    await expect(bare.listTools()).rejects.toThrow(/Method not found/);
  });
});
