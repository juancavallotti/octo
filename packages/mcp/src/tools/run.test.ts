import { describe, expect, it } from "vitest";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { registerRunTools } from "./run";
import { createNamespaceResolver } from "../namespace";
import type { OctoMcpConfig } from "../backend";
import type { RunHostPort, RunStatusLike } from "../run-host";

/** A stub run host that records the last start() and fakes exposable runs. */
function stubRunHost(opts: { available?: boolean } = {}) {
  const available = opts.available ?? true;
  const calls: { startedYaml?: string; startedEnv?: Record<string, string>; ns?: string } = {};
  let running = false;
  let exposable = false;
  let logs: { seq: number; text: string }[] = [];
  let nsSeq = 0;
  const snap = (): RunStatusLike => ({
    available,
    running,
    version: null,
    exposable,
    port: exposable && running ? 4000 : null,
    testPath: exposable && running ? `/editor/runs/${calls.ns}/` : null,
  });
  const host: RunHostPort = {
    status: () => snap(),
    start: async (ns, yaml, env) => {
      calls.ns = ns;
      calls.startedYaml = yaml;
      calls.startedEnv = env;
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
    snapshot: () => logs,
    newNamespace: () => `ns-${++nsSeq}`,
  };
  return { host, calls };
}

const REC = {
  id: "a",
  name: "Alpha",
  definition: "service:\n  name: Alpha\nconnectors:\n  - type: http\n    HTTP_PORT: 8080\n",
};

async function connect(
  config: OctoMcpConfig,
  runHost: RunHostPort,
): Promise<Client> {
  const server = new McpServer({ name: "octo-test", version: "0.0.0" });
  registerRunTools(server, config, runHost, createNamespaceResolver(runHost));
  const [clientT, serverT] = InMemoryTransport.createLinkedPair();
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

  it("run blocks an invalid definition", async () => {
    const { host } = stubRunHost();
    const client = await connect(
      config({ validate: () => ({ valid: false, errors: ["bad"] }) }),
      host,
    );
    const res = (await client.callTool({
      name: "run_integration",
      arguments: { id: "a" },
    })) as CallToolResult;
    expect(res.isError).toBe(true);
    expect(text(res)).toContain("bad");
  });

  it("run errors when no runner is available", async () => {
    const { host } = stubRunHost({ available: false });
    const client = await connect(config(), host);
    const res = (await client.callTool({
      name: "run_integration",
      arguments: { id: "a" },
    })) as CallToolResult;
    expect(res.isError).toBe(true);
    expect(text(res)).toContain("OCTO_BIN_PATH");
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

  it("stop returns running:false", async () => {
    const { host } = stubRunHost();
    const client = await connect(config(), host);
    await client.callTool({ name: "run_integration", arguments: { id: "a" } });
    const res = (await client.callTool({
      name: "stop_integration",
      arguments: {},
    })) as CallToolResult;
    expect(parse(res)).toEqual({ running: false });
  });

  it("get_run_logs returns buffered text, or a hint when empty", async () => {
    const { host } = stubRunHost();
    const client = await connect(config(), host);
    const empty = (await client.callTool({
      name: "get_run_logs",
      arguments: {},
    })) as CallToolResult;
    expect(text(empty)).toContain("no logs yet");
    await client.callTool({ name: "run_integration", arguments: { id: "a" } });
    const after = (await client.callTool({
      name: "get_run_logs",
      arguments: {},
    })) as CallToolResult;
    expect(text(after)).toContain("starting octo");
  });
});
