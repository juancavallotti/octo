// @vitest-environment node
import { afterEach, describe, expect, it } from "vitest";
import { GET } from "./route";

/**
 * The desktop shell asks this before it switches folders, which stops every run.
 * The distinction that matters is between a session that merely *exists* (the map
 * keeps a record per browser tab, with a log buffer, long after the run ended) and
 * one that is actually holding a process. Counting the former would warn the user
 * about work that finished an hour ago, and they would learn to click through it.
 */
type Store = { __octoRunSessions?: Map<string, unknown> };

function seed(entries: Record<string, boolean>) {
  const map = new Map<string, unknown>();
  for (const [ns, running] of Object.entries(entries)) {
    map.set(ns, { namespace: ns, proc: running ? { pid: 1 } : null });
  }
  (globalThis as Store).__octoRunSessions = map;
}

afterEach(() => {
  delete (globalThis as Store).__octoRunSessions;
});

async function body() {
  return (await (await GET()).json()) as { running: number; namespaces: string[] };
}

describe("GET /api/run/active", () => {
  it("reports nothing running on a fresh server", async () => {
    expect(await body()).toEqual({ running: 0, namespaces: [] });
  });

  it("counts only sessions holding a process", async () => {
    seed({ live: true, alsoLive: true, finished: false });
    const result = await body();
    expect(result.running).toBe(2);
    expect(result.namespaces.sort()).toEqual(["alsoLive", "live"]);
  });

  it("does not count a session whose run has ended", async () => {
    seed({ finished: false });
    expect(await body()).toEqual({ running: 0, namespaces: [] });
  });
});
