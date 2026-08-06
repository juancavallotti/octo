import { describe, expect, it } from "vitest";
import { createNamespaceResolver } from "./namespace";
import type { RunHostPort } from "./run-host";

function counterHost(): RunHostPort {
  let n = 0;
  return {
    status: () => ({ available: true, running: false, version: null, exposable: false, port: null, testUrl: null }),
    start: async () => ({ available: true, running: true, version: null, exposable: false, port: null, testUrl: null }),
    stop: async () => ({ available: true, running: false, version: null, exposable: false, port: null, testUrl: null }),
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
    snapshot: () => [],
    newNamespace: () => `ns-${++n}`,
  };
}

describe("createNamespaceResolver", () => {
  it("gives each session a stable, distinct namespace", () => {
    const resolve = createNamespaceResolver(counterHost());
    const a1 = resolve("session-a");
    const b1 = resolve("session-b");
    const a2 = resolve("session-a");
    expect(a1).toBe(a2); // stable within a session
    expect(a1).not.toBe(b1); // isolated across sessions
  });

  it("shares one lazily-minted namespace for sessionless callers", () => {
    const resolve = createNamespaceResolver(counterHost());
    const first = resolve(undefined);
    const second = resolve(undefined);
    expect(first).toBe(second);
  });
});
