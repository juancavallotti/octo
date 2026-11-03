import { describe, expect, it } from "vitest";
import { Client, StreamableHTTPClientTransport } from "@modelcontextprotocol/client";
import { createOctoMcpHandler } from "./handler";
import type { OctoMcpConfig } from "./backend";
import type { RunHostPort } from "./run-host";

/**
 * The route handler's wiring, driven over HTTP by a real client. The tools, resource
 * and prompts are covered in full via the in-memory client elsewhere; here we only
 * assert they are reachable through the handler, from a client of either protocol era.
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
    get: async () => ({ id: "", name: "", files: [] }),
    create: async () => ({ id: "", name: "", files: [] }),
    update: async () => ({ id: "", name: "", files: [] }),
  },
  validate: () => ({ valid: true, errors: [] }),
  runtimeSchema: {},
};

/** Connect a client whose every request is answered by `handler`, mounted at `url`. */
async function connect(
  handler: (request: Request) => Promise<Response>,
  url: string,
  mode?: "legacy" | { pin: string },
): Promise<Client> {
  const client = new Client(
    { name: "t", version: "0" },
    mode ? { versionNegotiation: { mode } } : undefined,
  );
  await client.connect(
    new StreamableHTTPClientTransport(new URL(url), {
      fetch: (input, init) => handler(new Request(input, init)),
    }),
  );
  return client;
}

describe("createOctoMcpHandler", () => {
  it("serves its tools to a 2026-07-28 client at whatever route it is mounted on", async () => {
    const handler = createOctoMcpHandler(config, { runHost: stubRunHost });
    const client = await connect(handler, "http://localhost/api/mcp", { pin: "2026-07-28" });
    const { tools } = await client.listTools();
    expect(tools.map((t) => t.name)).toContain("list_integrations");
    await client.close();
  });

  it("serves the same tools to a 2025-era client", async () => {
    const handler = createOctoMcpHandler(config, { runHost: stubRunHost });
    const client = await connect(handler, "http://localhost/mcp", "legacy");
    const { tools } = await client.listTools();
    expect(tools.map((t) => t.name)).toContain("list_integrations");
    await client.close();
  });

  it("reports the host's identity, icons included", async () => {
    const handler = createOctoMcpHandler(config, {
      runHost: stubRunHost,
      serverInfo: { name: "octo-test", title: "Octo Test" },
    });
    const client = await connect(handler, "http://localhost/mcp", "legacy");
    const info = client.getServerVersion();
    expect(info?.name).toBe("octo-test");
    expect(info?.title).toBe("Octo Test");
    expect(info?.icons?.length).toBeGreaterThan(0);
    await client.close();
  });
});
