import { describe, expect, it } from "vitest";
import { createNamespaceResolver } from "./namespace";
import type { RunHostPort } from "./run-host";

function counterHost(): RunHostPort {
  let n = 0;
  return {
    binaries: () => ({ available: true, version: null }),
    start: async () => ({ running: true, exposable: false, port: null, testUrl: null }),
    stop: async () => ({ running: false, exposable: false, port: null, testUrl: null }),
    invoke: async () => ({ ok: true, exitCode: 0, timedOut: false, dropped: false, output: "", logs: [] }),
    test: async () => ({
      ok: true,
      exitCode: 0,
      timedOut: false,
      totals: { cases: 0, passed: 0, failed: 0, errored: 0, skipped: 0, notRun: 0, elapsedMs: 0 },
      suites: [],
      logs: [],
    }),
    evalCel: async () => ({ ok: true, result: null, logs: [] }),
    snapshot: async () => [],
    newNamespace: () => `ns-${++n}`,
  };
}

describe("createNamespaceResolver", () => {
  const as = (userId: string, clientId?: string) => ({ clientId, extra: { userId } });

  it("gives each (user, client) a stable, distinct namespace", () => {
    const resolve = createNamespaceResolver(counterHost());
    const a1 = resolve(as("ada", "claude"));
    const a2 = resolve(as("ada", "claude"));
    expect(a1).toBe(a2); // stable across requests, so a refreshed token keeps its run
    expect(resolve(as("ada", "cursor"))).not.toBe(a1); // one user, two clients
    expect(resolve(as("bob", "claude"))).not.toBe(a1); // one client, two users
  });

  it("shares one lazily-minted namespace for unauthenticated callers", () => {
    const resolve = createNamespaceResolver(counterHost());
    const first = resolve(undefined);
    expect(resolve({})).toBe(first);
    expect(resolve({ clientId: "claude" })).toBe(first); // a client without a user is no caller
  });
});
