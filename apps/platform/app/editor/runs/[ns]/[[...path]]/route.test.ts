// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { createServer, type Server } from "node:http";
import { AddressInfo } from "node:net";
import { GET, POST } from "./route";

/** A minimal upstream that echoes how it was called, standing in for a running
 * networked integration on 127.0.0.1:<port>. */
function startUpstream(): Promise<{ server: Server; port: number }> {
  return new Promise((resolve) => {
    const server = createServer((req, res) => {
      let body = "";
      req.on("data", (c) => (body += c));
      req.on("end", () => {
        res.setHeader("content-type", "text/plain");
        res.end(`${req.method} ${req.url} body=${body}`);
      });
    });
    server.listen(0, "127.0.0.1", () => {
      resolve({ server, port: (server.address() as AddressInfo).port });
    });
  });
}

/** Seed the global session map so runningPort(ns) resolves to a fake running run. */
function seedRunning(ns: string, port: number): void {
  const store = globalThis as { __octoRunSessions?: Map<string, unknown> };
  store.__octoRunSessions = new Map([[ns, { proc: {}, port, exposable: true }]]);
}

const params = (ns: string, path?: string[]) => ({ params: Promise.resolve({ ns, path }) });

describe("run reverse proxy", () => {
  let server: Server;
  let port: number;

  beforeEach(async () => {
    ({ server, port } = await startUpstream());
  });

  afterEach(() => {
    server.close();
    (globalThis as { __octoRunSessions?: unknown }).__octoRunSessions = undefined;
  });

  it("forwards the path and query to the integration's port", async () => {
    seedRunning("aaaa0000", port);
    const req = new Request("http://editor.local/editor/runs/aaaa0000/orders?limit=5");
    const res = await GET(req, params("aaaa0000", ["orders"]));
    expect(res.status).toBe(200);
    expect(await res.text()).toBe("GET /orders?limit=5 body=");
  });

  it("forwards the request body on POST", async () => {
    seedRunning("aaaa0000", port);
    const req = new Request("http://editor.local/editor/runs/aaaa0000/ingest", {
      method: "POST",
      body: "hello",
    });
    const res = await POST(req, params("aaaa0000", ["ingest"]));
    expect(await res.text()).toBe("POST /ingest body=hello");
  });

  it("proxies the run root (no path segments)", async () => {
    seedRunning("aaaa0000", port);
    const req = new Request("http://editor.local/editor/runs/aaaa0000/");
    const res = await GET(req, params("aaaa0000", undefined));
    expect(await res.text()).toBe("GET / body=");
  });

  it("404s when the run is not running", async () => {
    const req = new Request("http://editor.local/editor/runs/bbbb1111/x");
    const res = await GET(req, params("bbbb1111", ["x"]));
    expect(res.status).toBe(404);
  });

  it("404s on a malformed namespace", async () => {
    const req = new Request("http://editor.local/editor/runs/..%2Fetc/x");
    const res = await GET(req, params("../etc", ["x"]));
    expect(res.status).toBe(404);
  });
});
