import { describe, expect, it } from "vitest";
import { createOctoMcpHandler } from "./handler";
import type { OctoMcpConfig } from "./backend";
import type { RunHostPort } from "./run-host";

/**
 * Smoke tests for the route handler's wiring. The tools/resource/prompts are
 * covered in full via the in-memory client elsewhere; here we only assert
 * mcp-handler is mounted and routes correctly. We deliberately avoid a full
 * `initialize` (its streamable response is a long-lived SSE stream that would hang
 * `res.text()`), exercising the fast error paths instead.
 */

/** An empty tally, for a stub that never runs anything. */
const NO_TOTALS = {
  cases: 0,
  passed: 0,
  failed: 0,
  errored: 0,
  skipped: 0,
  notRun: 0,
  elapsedMs: 0,
};

const stubRunHost: RunHostPort = {
  binaries: () => ({ available: false, version: null }),
  start: async () => ({ running: false, exposable: false, port: null, testUrl: null }),
  stop: async () => ({ running: false, exposable: false, port: null, testUrl: null }),
  invoke: async () => ({ ok: false, exitCode: null, timedOut: false, dropped: false, output: "", logs: [] }),
  test: async () => ({ ok: false, exitCode: null, timedOut: false, totals: NO_TOTALS, suites: [], logs: [] }),
  evalCel: async () => ({ ok: false, error: "unavailable", logs: [] }),
  snapshot: async () => [],
  newNamespace: () => "ns-1",
};

const config: OctoMcpConfig = {
  store: {
    list: async () => [],
    get: async () => ({ id: "", name: "", definition: "" }),
    create: async () => ({ id: "", name: "", definition: "" }),
    update: async () => ({ id: "", name: "", definition: "" }),
  },
  validate: () => ({ valid: true, errors: [] }),
  runtimeSchema: {},
};

const initBody = JSON.stringify({
  jsonrpc: "2.0",
  id: 1,
  method: "initialize",
  params: { protocolVersion: "2025-06-18", capabilities: {}, clientInfo: { name: "t", version: "0" } },
});

describe("createOctoMcpHandler", () => {
  it("returns a request handler function", () => {
    expect(typeof createOctoMcpHandler(config, { runHost: stubRunHost })).toBe("function");
  });

  it("serves the streamable endpoint at <basePath>/mcp", async () => {
    const handler = createOctoMcpHandler(config, { runHost: stubRunHost });
    // Missing text/event-stream in Accept -> the transport rejects fast with a
    // JSON-RPC 406, proving the request reached the mounted MCP transport.
    const res = await handler(
      new Request("http://localhost/mcp", {
        method: "POST",
        headers: { "content-type": "application/json", accept: "application/json" },
        body: initBody,
      }),
    );
    expect(res.status).toBe(406);
    const json = (await res.json()) as { error?: { message?: string } };
    expect(json.error?.message).toContain("text/event-stream");
  });

  it("honors basePath when deriving the endpoint", async () => {
    const handler = createOctoMcpHandler(config, { runHost: stubRunHost, basePath: "/api" });
    const reach = async (url: string) =>
      (
        await handler(
          new Request(url, {
            method: "POST",
            headers: { "content-type": "application/json", accept: "application/json" },
            body: initBody,
          }),
        )
      ).status;
    expect(await reach("http://localhost/api/mcp")).toBe(406); // mounted here
    expect(await reach("http://localhost/mcp")).toBe(404); // not at the bare path
  });
});
