// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { mkdtemp, writeFile, chmod } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { allSessions, start, status, stop, type Session } from "./localRunner";
import { ensureReaper, reapIdle } from "./reaper";

async function fakeBin(dir: string, name: string, body: string): Promise<string> {
  const path = join(dir, name);
  await writeFile(path, `#!/bin/sh\n${body}\n`, "utf8");
  await chmod(path, 0o755);
  return path;
}

const NS = "reaper00";

describe("idle reaper", () => {
  let dir: string;

  beforeEach(async () => {
    dir = await mkdtemp(join(tmpdir(), "octo-reaper-"));
    process.env.OCTO_RUN_DIR = dir;
  });

  afterEach(async () => {
    await stop(NS);
    allSessions().delete(NS);
    delete process.env.OCTO_BIN_PATH;
    delete process.env.OCTO_RUN_DIR;
  });

  it("stops and forgets a run idle past the timeout", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-sleep", "sleep 5");
    const started = await start(NS, "service:\n  name: idle\n");
    expect(started.running).toBe(true);

    // Backdate activity beyond the 1h window, then sweep.
    allSessions().get(NS)!.lastActivity = 0;
    await reapIdle();

    // The session was removed; a fresh one reads as not-running with no logs.
    expect(status(NS).running).toBe(false);
    expect(allSessions().get(NS)?.proc ?? null).toBeNull();
  });

  it("leaves an active run alone", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-sleep", "sleep 5");
    await start(NS, "service:\n  name: busy\n");

    // lastActivity is current (just started); a sweep must not touch it.
    await reapIdle();
    expect(status(NS).running).toBe(true);
  });

  it("does nothing when nothing is idle", async () => {
    await expect(reapIdle()).resolves.toBeUndefined();
  });

  // The interval must swallow a sweep that throws. Without the guard, void reapIdle()
  // turns a failed sweep into an unhandled rejection, which Node escalates to a
  // process-ending exception. Drive one tick with a session whose teardown throws and
  // assert the interval caught it rather than letting it escape.
  it("swallows a failing sweep instead of crashing the interval", async () => {
    const reaperStore = globalThis as unknown as {
      __octoRunReaper?: ReturnType<typeof setInterval>;
    };
    // Drop any interval an earlier test created so ensureReaper builds a fresh one under
    // fake timers we can drive deterministically.
    if (reaperStore.__octoRunReaper) {
      clearInterval(reaperStore.__octoRunReaper);
      reaperStore.__octoRunReaper = undefined;
    }
    vi.useFakeTimers();
    const errors = vi.spyOn(console, "error").mockImplementation(() => {});
    const boom = {
      namespace: "boom0000",
      lastActivity: 0, // long idle → swept this tick
      proc: null,
      exit: null,
      configPath: null,
      logs: {
        push() {
          throw new Error("teardown blew up");
        },
      },
      port: null,
      adminPort: null,
      exposable: false,
      stagedResources: [],
      declaredResources: [],
    } as unknown as Session;
    allSessions().set("boom0000", boom);

    try {
      ensureReaper();
      // One REAPER_INTERVAL_MS sweep; the async form also flushes the .catch microtask.
      await vi.advanceTimersByTimeAsync(60 * 1000);
      expect(errors).toHaveBeenCalled();
    } finally {
      if (reaperStore.__octoRunReaper) clearInterval(reaperStore.__octoRunReaper);
      reaperStore.__octoRunReaper = undefined;
      allSessions().delete("boom0000");
      errors.mockRestore();
      vi.useRealTimers();
    }
  });
});
