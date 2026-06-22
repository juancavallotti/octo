// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { mkdtemp, writeFile, chmod } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { allSessions, start, status, stop } from "./session";
import { reapIdle } from "./reaper";

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
});
