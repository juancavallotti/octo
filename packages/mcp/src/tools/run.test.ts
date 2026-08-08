import { describe, expect, it } from "vitest";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { registerRunTools } from "./run";
import { createNamespaceResolver } from "../namespace";
import type { OctoMcpConfig } from "../backend";
import type { RunHostPort, RunKey, RunStateLike } from "../run-host";
import type { MockSpec, ResourceProvider, SpyTrace } from "@octo/run-host";

/**
 * A stub run host that records the last start() and fakes exposable runs.
 *
 * `available` is what binaries() reports — the LOCAL one-shot binary, which the one-shots
 * gate on. `startError`, when set, makes start() throw it: that models a LOCAL runner
 * whose octo binary is missing (start throws), as distinct from a REMOTE runner (the
 * platform's) whose start needs no local binary and succeeds even when available is false.
 */
function stubRunHost(opts: { available?: boolean; startError?: string } = {}) {
  const available = opts.available ?? true;
  const calls: {
    startedYaml?: string;
    startedEnv?: Record<string, string>;
    startedResources?: ResourceProvider;
    ns?: string;
    /** The whole key the long-running half was addressed with. */
    key?: RunKey;
    invoked?: {
      yaml: string;
      flow: string;
      opts?: {
        data?: string;
        vars?: string;
        env?: Record<string, string>;
        timeoutMs?: number;
        resources?: ResourceProvider;
        breakAt?: string;
        spies?: string[];
        mocks?: Record<string, MockSpec>;
        logLevel?: "debug" | "info" | "warn" | "error";
      };
    };
    evaluated?: {
      expression: string;
      opts?: {
        data?: string;
        vars?: string;
        env?: Record<string, string>;
        timeoutMs?: number;
      };
    };
  } = {};
  let running = false;
  let exposable = false;
  let logs: { seq: number; text: string }[] = [];
  let nsSeq = 0;
  const snap = (): RunStateLike => ({
    running,
    exposable,
    port: exposable && running ? 4000 : null,
    testUrl: exposable && running ? `/editor/runs/${calls.ns}/` : null,
  });
  const host: RunHostPort = {
    binaries: () => ({ available, version: null }),
    start: async (key, yaml, env, startOpts) => {
      if (opts.startError) throw new Error(opts.startError);
      calls.key = key;
      calls.ns = key.namespace;
      calls.startedYaml = yaml;
      calls.startedEnv = env;
      calls.startedResources = startOpts?.resources;
      running = true;
      exposable = yaml.includes("HTTP_PORT");
      logs = [{ seq: 0, text: "▶ starting octo" }];
      return snap();
    },
    stop: async () => {
      running = false;
      exposable = false;
      return snap();
    },
    invoke: async (ns, yaml, flow, opts) => {
      calls.ns = ns;
      calls.invoked = { yaml, flow, opts };

      // Spies are read-only, so they only ADD to the outcome — a spied run still reports
      // whatever it would have reported otherwise. One record per spied block.
      const spies: SpyTrace[] | undefined = opts?.spies?.map((address) => ({
        address,
        records: [
          {
            seq: 1,
            at: "2026-07-11T00:00:00Z",
            input: { body: { amount: 250 } },
            output: { body: { amount: 250 }, variables: { tier: "gold" } },
          },
        ],
      }));

      // A breakAt run halts at the block and reports the message there instead of a
      // result body, exactly as the real runner's envelope does.
      if (opts?.breakAt) {
        return {
          ok: true,
          exitCode: 0,
          timedOut: false,
          dropped: false,
          output: "",
          logs: ["invoked greet"],
          breakpoint: {
            reached: true,
            block: opts.breakAt,
            message: { body: { amount: 250 } },
          },
          spies,
        };
      }
      return {
        ok: true,
        exitCode: 0,
        timedOut: false,
        dropped: false,
        output: '{"result":"ok"}',
        logs: ["invoked greet"],
        spies,
      };
    },
    // The run tools never call it; the suite tools have their own harness.
    test: async () => ({
      ok: true,
      exitCode: 0,
      timedOut: false,
      totals: { cases: 0, passed: 0, failed: 0, errored: 0, skipped: 0, notRun: 0, elapsedMs: 0 },
      suites: [],
      logs: [],
    }),
    evalCel: async (expression, opts) => {
      calls.evaluated = { expression, opts };
      return { ok: true, result: true, logs: ["evaluated"] };
    },
    snapshot: async () => logs,
    newNamespace: () => `ns-${++nsSeq}`,
  };
  return { host, calls };
}

const REC = {
  id: "a",
  name: "Alpha",
  definition: "service:\n  name: Alpha\nconnectors:\n  - type: http\n    HTTP_PORT: 8080\n",
};

/**
 * Connect a client to the run tools.
 *
 * `userId` stands in for a verified bearer token: the real transport hands the tools an
 * `AuthInfo` the auth layer built, and the in-memory one carries whatever `send` is given —
 * so it is attached there rather than passed as a tool argument, which is the whole point
 * of the property being tested. Omitted, it models an unauthenticated host.
 */
async function connect(
  config: OctoMcpConfig,
  runHost: RunHostPort,
  userId?: string,
): Promise<Client> {
  const server = new McpServer({ name: "octo-test", version: "0.0.0" });
  registerRunTools(server, config, runHost, createNamespaceResolver(runHost));
  const [clientT, serverT] = InMemoryTransport.createLinkedPair();
  if (userId !== undefined) {
    const send = clientT.send.bind(clientT);
    const authInfo = { token: "t", clientId: "c", scopes: [], extra: { userId } };
    clientT.send = (message, options) => send(message, { ...options, authInfo });
  }
  const client = new Client({ name: "test-client", version: "0.0.0" });
  await Promise.all([server.connect(serverT), client.connect(clientT)]);
  return client;
}

function singleStore(rec = REC) {
  return {
    list: async () => [{ id: rec.id, name: rec.name }],
    get: async (id: string) => {
      if (id !== rec.id) throw new Error(`no such integration: ${id}`);
      return { ...rec };
    },
    create: async () => rec,
    update: async () => rec,
  };
}

function config(
  over: Partial<OctoMcpConfig> = {},
): OctoMcpConfig {
  return {
    store: singleStore(),
    validate: () => ({ valid: true, errors: [] }),
    runtimeSchema: {},
    ...over,
  };
}

function parse(res: CallToolResult): any {
  return JSON.parse((res.content as { text: string }[])[0].text);
}
function text(res: CallToolResult): string {
  return (res.content as { text: string }[])[0].text;
}

describe("run tools", () => {
  it("can_start reports available + validity", async () => {
    const { host } = stubRunHost();
    const client = await connect(
      config({ validate: () => ({ valid: false, errors: ["missing flow"] }) }),
      host,
    );
    const res = (await client.callTool({
      name: "can_start_integration",
      arguments: { id: "a" },
    })) as CallToolResult;
    expect(parse(res)).toEqual({ available: true, valid: false, errors: ["missing flow"] });
  });

  it("run returns an absolute test URL for a networked integration", async () => {
    const { host } = stubRunHost();
    const client = await connect(config({ baseUrl: "http://localhost:3000/" }), host);
    const res = (await client.callTool({
      name: "run_integration",
      arguments: { id: "a" },
    })) as CallToolResult;
    const out = parse(res);
    expect(out.running).toBe(true);
    expect(out.exposable).toBe(true);
    expect(out.testUrl).toBe(`http://localhost:3000/editor/runs/${out.namespace}/`);
  });

  it("run proceeds even when validation reports issues (runtime is the judge)", async () => {
    const { host } = stubRunHost();
    const client = await connect(
      config({ validate: () => ({ valid: false, errors: ["bad"] }) }),
      host,
    );
    const res = (await client.callTool({
      name: "run_integration",
      arguments: { id: "a" },
    })) as CallToolResult;
    expect(res.isError).toBeFalsy();
    expect(parse(res).running).toBe(true);
  });

  // A LOCAL runner reports a missing binary by throwing from start(); the run tool must
  // surface that as the error result rather than pre-gating on binaries().available.
  it("run surfaces a local runner's missing-binary error from start()", async () => {
    const { host } = stubRunHost({
      available: false,
      startError: "OCTO_BIN_PATH is not set; launch the editor with `task dev`.",
    });
    const client = await connect(config(), host);
    const res = (await client.callTool({
      name: "run_integration",
      arguments: { id: "a" },
    })) as CallToolResult;
    expect(res.isError).toBe(true);
    expect(text(res)).toContain("OCTO_BIN_PATH");
  });

  // The finding this guards: a REMOTE runner (the platform's, backed by the orchestrator)
  // starts a run with no local octo at all. Gating on binaries().available — the local
  // one-shot binary — would wrongly reject it and break platform dev runs.
  it("run starts on a remote host that has no local binary", async () => {
    const { host } = stubRunHost({ available: false }); // no startError: start() succeeds
    const client = await connect(config(), host);
    const res = (await client.callTool({
      name: "run_integration",
      arguments: { id: "a" },
    })) as CallToolResult;
    expect(res.isError).toBeFalsy();
    expect(parse(res).running).toBe(true);
  });

  it("run rejects an invalid env shape", async () => {
    const { host } = stubRunHost();
    const client = await connect(config(), host);
    const res = (await client.callTool({
      name: "run_integration",
      arguments: { id: "a", env: { "1bad": "x" } },
    })) as CallToolResult;
    expect(res.isError).toBe(true);
    expect(text(res)).toContain("invalid env");
  });

  it("run forwards a valid env to the runner", async () => {
    const { host, calls } = stubRunHost();
    const client = await connect(config(), host);
    await client.callTool({
      name: "run_integration",
      arguments: { id: "a", env: { API_KEY: "secret" } },
    });
    expect(calls.startedEnv).toEqual({ API_KEY: "secret" });
  });

  it("run threads a resource provider bound to the integration id", async () => {
    const { host, calls } = stubRunHost();
    const provider: ResourceProvider = async () => [];
    const seen: (string | undefined)[] = [];
    const client = await connect(
      config({
        resources: (id) => {
          seen.push(id);
          return provider;
        },
      }),
      host,
    );
    await client.callTool({ name: "run_integration", arguments: { id: "a" } });
    expect(seen).toEqual(["a"]);
    expect(calls.startedResources).toBe(provider);
  });

  // The three halves of a run's address, and where each comes from. The user matters most:
  // a host that scopes runs per user must take it from the verified token and never from
  // the arguments, or one client could name another's run.
  it("addresses the run by the session, the integration, and the caller", async () => {
    const { host, calls } = stubRunHost();
    const client = await connect(config(), host, "user-7");

    await client.callTool({ name: "run_integration", arguments: { id: "a" } });

    expect(calls.key).toEqual({
      namespace: expect.stringMatching(/^ns-/),
      integrationId: "a",
      userId: "user-7",
    });
  });

  // An unauthenticated host (the standalone app) has no caller to name, and keys on the
  // session alone — so the absence has to travel rather than being invented.
  it("leaves the caller out when the host has no auth", async () => {
    const { host, calls } = stubRunHost();
    const client = await connect(config(), host);

    await client.callTool({ name: "run_integration", arguments: { id: "a" } });

    expect(calls.key?.userId).toBeUndefined();
  });

  it("stop returns running:false", async () => {
    const { host } = stubRunHost();
    const client = await connect(config(), host);
    await client.callTool({ name: "run_integration", arguments: { id: "a" } });
    const res = (await client.callTool({
      name: "stop_integration",
      arguments: { id: "a" },
    })) as CallToolResult;
    expect(parse(res)).toEqual({ running: false });
  });

  it("get_run_logs returns buffered text, or a hint when empty", async () => {
    const { host } = stubRunHost();
    const client = await connect(config(), host);
    const empty = (await client.callTool({
      name: "get_run_logs",
      arguments: { id: "a" },
    })) as CallToolResult;
    expect(text(empty)).toContain("no logs yet");
    await client.callTool({ name: "run_integration", arguments: { id: "a" } });
    const after = (await client.callTool({
      name: "get_run_logs",
      arguments: { id: "a" },
    })) as CallToolResult;
    expect(text(after)).toContain("starting octo");
  });

  it("invoke_flow returns the flow output and logs", async () => {
    const { host } = stubRunHost();
    const client = await connect(config(), host);
    const res = (await client.callTool({
      name: "invoke_flow",
      arguments: { id: "a", flow: "greet" },
    })) as CallToolResult;
    expect(res.isError).toBeFalsy();
    expect(parse(res)).toEqual({
      ok: true,
      timedOut: false,
      dropped: false,
      output: '{"result":"ok"}',
      logs: ["invoked greet"],
    });
  });

  it("invoke_flow forwards vars, breakAt and logLevel to the runner", async () => {
    const { host, calls } = stubRunHost();
    const client = await connect(config(), host);
    await client.callTool({
      name: "invoke_flow",
      arguments: {
        id: "a",
        flow: "greet",
        vars: '{"tier":"gold"}',
        breakAt: "greet.charge",
        logLevel: "error",
      },
    });
    expect(calls.invoked?.opts?.vars).toBe('{"tier":"gold"}');
    expect(calls.invoked?.opts?.breakAt).toBe("greet.charge");
    expect(calls.invoked?.opts?.logLevel).toBe("error");
  });

  it("invoke_flow surfaces the breakpoint envelope", async () => {
    const { host } = stubRunHost();
    const client = await connect(config(), host);
    const res = (await client.callTool({
      name: "invoke_flow",
      arguments: { id: "a", flow: "greet", breakAt: "greet.charge" },
    })) as CallToolResult;
    expect(res.isError).toBeFalsy();
    expect(parse(res)).toMatchObject({
      ok: true,
      breakpoint: {
        reached: true,
        block: "greet.charge",
        message: { body: { amount: 250 } },
      },
    });
  });

  it("invoke_flow forwards spies and mocks to the runner", async () => {
    const { host, calls } = stubRunHost();
    const client = await connect(config(), host);
    await client.callTool({
      name: "invoke_flow",
      arguments: {
        id: "a",
        flow: "greet",
        spies: ["greet.seed", "greet.fanout[audit].log-it"],
        mocks: {
          "greet.charge": {
            cases: [{ when: "body.amount > 100", error: "card declined" }],
            default: { body: { ok: true } },
          },
        },
      },
    });
    expect(calls.invoked?.opts?.spies).toEqual([
      "greet.seed",
      "greet.fanout[audit].log-it",
    ]);
    expect(calls.invoked?.opts?.mocks).toEqual({
      "greet.charge": {
        cases: [{ when: "body.amount > 100", error: "card declined" }],
        default: { body: { ok: true } },
      },
    });
  });

  it("invoke_flow surfaces the spy traces", async () => {
    const { host } = stubRunHost();
    const client = await connect(config(), host);
    const res = (await client.callTool({
      name: "invoke_flow",
      arguments: { id: "a", flow: "greet", spies: ["greet.seed"] },
    })) as CallToolResult;
    expect(res.isError).toBeFalsy();
    expect(parse(res)).toMatchObject({
      ok: true,
      // A spy is read-only: the run still reports the result it would have reported.
      output: '{"result":"ok"}',
      spies: [
        {
          address: "greet.seed",
          records: [{ seq: 1, input: { body: { amount: 250 } } }],
        },
      ],
    });
  });

  it("invoke_flow errors when no runner is available", async () => {
    const { host } = stubRunHost({ available: false });
    const client = await connect(config(), host);
    const res = (await client.callTool({
      name: "invoke_flow",
      arguments: { id: "a", flow: "greet" },
    })) as CallToolResult;
    expect(res.isError).toBe(true);
    expect(text(res)).toContain("OCTO_BIN_PATH");
  });

  it("invoke_flow rejects an invalid env shape", async () => {
    const { host } = stubRunHost();
    const client = await connect(config(), host);
    const res = (await client.callTool({
      name: "invoke_flow",
      arguments: { id: "a", flow: "greet", env: { "1bad": "x" } },
    })) as CallToolResult;
    expect(res.isError).toBe(true);
    expect(text(res)).toContain("invalid env");
  });

  it("invoke_flow forwards flow, data, env, and timeout to the runner", async () => {
    const { host, calls } = stubRunHost();
    const client = await connect(config(), host);
    await client.callTool({
      name: "invoke_flow",
      arguments: {
        id: "a",
        flow: "greet",
        data: '{"x":1}',
        env: { API_KEY: "secret" },
        timeoutMs: 5000,
      },
    });
    expect(calls.invoked?.yaml).toBe(REC.definition);
    expect(calls.invoked?.flow).toBe("greet");
    expect(calls.invoked?.opts).toEqual({
      data: '{"x":1}',
      env: { API_KEY: "secret" },
      timeoutMs: 5000,
    });
  });

  it("invoke_flow runs an inline definition without touching the store", async () => {
    const { host, calls } = stubRunHost();
    const client = await connect(config(), host);
    const inline = "service:\n  name: Draft\n";
    const res = (await client.callTool({
      name: "invoke_flow",
      arguments: { definition: inline, flow: "greet" },
    })) as CallToolResult;
    expect(res.isError).toBeFalsy();
    expect(calls.invoked?.yaml).toBe(inline);
  });

  it("invoke_flow threads resources by id, and asks the host with no id for an inline definition", async () => {
    const byId = stubRunHost();
    const idProvider: ResourceProvider = async () => [];
    const inlineProvider: ResourceProvider = async () => [];
    const seen: (string | undefined)[] = [];
    const resources = (id?: string) => {
      seen.push(id);
      return id ? idProvider : inlineProvider;
    };

    const idClient = await connect(config({ resources }), byId.host);
    await idClient.callTool({
      name: "invoke_flow",
      arguments: { id: "a", flow: "greet" },
    });
    expect(byId.calls.invoked?.opts?.resources).toBe(idProvider);

    const inline = stubRunHost();
    const inlineClient = await connect(config({ resources }), inline.host);
    await inlineClient.callTool({
      name: "invoke_flow",
      arguments: { definition: "service:\n  name: Draft\n", flow: "greet" },
    });
    expect(inline.calls.invoked?.opts?.resources).toBe(inlineProvider);

    expect(seen).toEqual(["a", undefined]);
  });

  it("invoke_flow errors when neither id nor definition is given", async () => {
    const { host } = stubRunHost();
    const client = await connect(config(), host);
    const res = (await client.callTool({
      name: "invoke_flow",
      arguments: { flow: "greet" },
    })) as CallToolResult;
    expect(res.isError).toBe(true);
    expect(text(res)).toContain("exactly one of id or definition");
  });

  it("invoke_flow errors when both id and definition are given", async () => {
    const { host } = stubRunHost();
    const client = await connect(config(), host);
    const res = (await client.callTool({
      name: "invoke_flow",
      arguments: { id: "a", definition: "service:\n  name: X\n", flow: "greet" },
    })) as CallToolResult;
    expect(res.isError).toBe(true);
    expect(text(res)).toContain("exactly one of id or definition");
  });

  it("invoke_flow errors on an unknown integration id", async () => {
    const { host } = stubRunHost();
    const client = await connect(config(), host);
    const res = (await client.callTool({
      name: "invoke_flow",
      arguments: { id: "nope", flow: "greet" },
    })) as CallToolResult;
    expect(res.isError).toBe(true);
    expect(text(res)).toContain("no such integration");
  });

  it("evaluate_cel returns the ok/result/error envelope", async () => {
    const { host } = stubRunHost();
    const client = await connect(config(), host);
    const res = (await client.callTool({
      name: "evaluate_cel",
      arguments: { expression: "body.amount > 100", data: '{"amount":150}' },
    })) as CallToolResult;
    expect(res.isError).toBeFalsy();
    expect(parse(res)).toEqual({ ok: true, result: true });
  });

  it("evaluate_cel forwards expression, data, vars, env, and timeout", async () => {
    const { host, calls } = stubRunHost();
    const client = await connect(config(), host);
    await client.callTool({
      name: "evaluate_cel",
      arguments: {
        expression: "vars.tier == env.REGION",
        data: '{"x":1}',
        vars: '{"tier":"us"}',
        env: { REGION: "us" },
        timeoutMs: 2000,
      },
    });
    expect(calls.evaluated?.expression).toBe("vars.tier == env.REGION");
    expect(calls.evaluated?.opts).toEqual({
      data: '{"x":1}',
      vars: '{"tier":"us"}',
      env: { REGION: "us" },
      timeoutMs: 2000,
    });
  });

  it("evaluate_cel errors when no runner is available", async () => {
    const { host } = stubRunHost({ available: false });
    const client = await connect(config(), host);
    const res = (await client.callTool({
      name: "evaluate_cel",
      arguments: { expression: "body" },
    })) as CallToolResult;
    expect(res.isError).toBe(true);
    expect(text(res)).toContain("OCTO_BIN_PATH");
  });

  it("evaluate_cel rejects an invalid env shape", async () => {
    const { host } = stubRunHost();
    const client = await connect(config(), host);
    const res = (await client.callTool({
      name: "evaluate_cel",
      arguments: { expression: "env.X", env: { "1bad": "x" } },
    })) as CallToolResult;
    expect(res.isError).toBe(true);
    expect(text(res)).toContain("invalid env");
  });
});
