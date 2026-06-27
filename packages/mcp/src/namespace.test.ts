import { describe, expect, it } from "vitest";
import { createNamespaceResolver } from "./namespace";
import type { RunHostPort } from "./run-host";

function counterHost(): RunHostPort {
  let n = 0;
  return {
    status: () => ({ available: true, running: false, version: null, exposable: false, port: null, testPath: null }),
    start: async () => ({ available: true, running: true, version: null, exposable: false, port: null, testPath: null }),
    stop: async () => ({ available: true, running: false, version: null, exposable: false, port: null, testPath: null }),
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
