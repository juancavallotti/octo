// @vitest-environment node
import { afterEach, describe, expect, it } from "vitest";
import { deriveNamespace, newNamespace } from "@octo/run-host";
import { GET } from "./route";

/**
 * The log stream is the one part of RUN that cannot send its tab id as an argument —
 * an EventSource sets no headers — so it rides in the query string. That plumbing is
 * exactly the kind that breaks quietly: the stream still opens, it just replays the
 * wrong tab's buffer.
 */

type SessionStore = { __octoRunSessions?: Map<string, unknown> };

const COOKIE = "aaaa0000";
const TAB = "3f9c1e7b8a4d2c5e6f0b1a9d8c7e6f5a";

/** Seed a namespace's buffer directly, the way the run-proxy route test does. */
function seed(ns: string, text: string) {
  const store = globalThis as SessionStore;
  const map = store.__octoRunSessions ?? new Map();
  map.set(ns, {
    namespace: ns,
    lastActivity: 0,
    proc: null,
    logs: {
      snapshot: () => [{ seq: 1, text }],
      subscribe: () => () => {},
    },
  });
  store.__octoRunSessions = map;
}

/** Read the replayed lines, then drop the stream so the route's cleanup runs. */
async function firstChunk(res: Response): Promise<string> {
  const reader = res.body!.getReader();
  const { value } = await reader.read();
  await reader.cancel();
  return new TextDecoder().decode(value);
}

function request(query: string): Request {
  return new Request(`http://editor.local/api/run/logs${query}`, {
    headers: { cookie: `octo_ns=${COOKIE}` },
  });
}

afterEach(() => {
  (globalThis as SessionStore).__octoRunSessions = undefined;
});

describe("GET /api/run/logs", () => {
  it("streams the namespace derived from the cookie and the tab", async () => {
    seed(deriveNamespace(COOKIE, TAB), "from this tab");

    expect(await firstChunk(await GET(request(`?tab=${TAB}`)))).toContain("from this tab");
  });

  // Same browser, two tabs: the cookie matches but the derived namespaces do not, so
  // one tab's Run panel must never show the other's output.
  it("keeps two tabs of one browser on separate streams", async () => {
    const other = "0000000000000000000000000000000b";
    seed(deriveNamespace(COOKIE, TAB), "from this tab");
    seed(deriveNamespace(COOKIE, other), "from the other tab");

    const chunk = await firstChunk(await GET(request(`?tab=${other}`)));
    expect(chunk).toContain("from the other tab");
    expect(chunk).not.toContain("from this tab");
  });

  // A caller with no tab id — a stale bundle, curl — gets the plain cookie namespace,
  // which is how the stream behaved before tabs were separated.
  it("falls back to the cookie namespace when no tab is given", async () => {
    seed(COOKIE, "cookie-only");

    expect(await firstChunk(await GET(request("")))).toContain("cookie-only");
  });

  it("mints a namespace cookie for a browser that has none", async () => {
    const res = await GET(new Request("http://editor.local/api/run/logs"));
    await res.body!.cancel();

    expect(res.headers.get("Set-Cookie")).toMatch(/^octo_ns=[a-z0-9]{8}; .*HttpOnly/);
  });
});

// Guards the assumption the other cases rest on: that a tab id actually moves the
// namespace off the cookie's own.
describe("deriveNamespace", () => {
  it("puts each tab somewhere other than the plain cookie namespace", () => {
    const base = newNamespace();
    expect(deriveNamespace(base, TAB)).not.toBe(base);
  });
});
