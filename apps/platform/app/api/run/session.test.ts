// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { mkdtemp, readFile, writeFile, chmod, readdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  currentConfigPath,
  snapshot,
  start,
  status,
  stop,
  sync,
} from "./session";

/** Fixed namespace for the single-user test surface. */
const NS = "testns00";

/** Writes an executable shell script acting as a stand-in for the octo binary. */
async function fakeBin(dir: string, name: string, body: string): Promise<string> {
  const path = join(dir, name);
  await writeFile(path, `#!/bin/sh\n${body}\n`, "utf8");
  await chmod(path, 0o755);
  return path;
}

const texts = () => snapshot(NS).map((l) => l.text);

describe("run session", () => {
  let dir: string;

  beforeEach(async () => {
    dir = await mkdtemp(join(tmpdir(), "octo-session-"));
    process.env.OCTO_RUN_DIR = dir;
  });

  afterEach(async () => {
    await stop(NS);
    delete process.env.OCTO_BIN_PATH;
    delete process.env.OCTO_RUN_DIR;
  });

  it("reports availability from OCTO_BIN_PATH", () => {
    delete process.env.OCTO_BIN_PATH;
    expect(status(NS).available).toBe(false);
    process.env.OCTO_BIN_PATH = "/somewhere/octo";
    expect(status(NS).available).toBe(true);
  });

  it("captures runner output line-by-line and tracks exit", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(
      dir,
      "octo-print",
      'printf "line one\\nline two\\n"',
    );

    const started = await start(NS, "service:\n  name: t\n");
    expect(started.running).toBe(true);

    await vi.waitFor(
      () => {
        const out = texts();
        expect(out).toContain("line one");
        expect(out).toContain("line two");
        expect(out.some((t) => t.startsWith("■ runner exited"))).toBe(true);
      },
      { timeout: 4000 },
    );

    expect(status(NS).running).toBe(false);
    expect(texts().some((t) => t.startsWith("▶ starting octo"))).toBe(true);
  });

  it("hot-reloads by rewriting the same config file on sync", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(
      dir,
      "octo-sleep",
      'echo ready\nsleep 2',
    );

    await start(NS, "service:\n  name: first\n");
    await vi.waitFor(() => expect(texts()).toContain("ready"), { timeout: 4000 });

    const path = currentConfigPath(NS);
    expect(path).toBeTruthy();
    expect(await readFile(path!, "utf8")).toContain("first");

    await sync(NS, "service:\n  name: second\n");
    expect(await readFile(path!, "utf8")).toContain("second");

    const stopped = await stop(NS);
    expect(stopped.running).toBe(false);
    // The rendered config (in the namespace's directory) is cleaned up on stop.
    expect(await readdir(join(dir, NS))).not.toContain(path!.split("/").pop());
  });

  it("allocates a port and injects HTTP_PORT for a networked run", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(
      dir,
      "octo-port",
      'printf "bound %s on %s\\n" "$HTTP_PORT" "$HTTP_HOST"',
    );

    const yaml =
      "service:\n  name: net\nenv:\n  - name: HTTP_PORT\n    default: \"8080\"\n";
    const started = await start(NS, yaml);
    expect(started.exposable).toBe(true);
    expect(started.port).toBeGreaterThanOrEqual(40000);

    await vi.waitFor(
      () => {
        expect(texts()).toContain(`bound ${started.port} on 127.0.0.1`);
      },
      { timeout: 4000 },
    );

    // The port is released once the run is stopped.
    await stop(NS);
    expect(status(NS).port).toBeNull();
    expect(status(NS).exposable).toBe(false);
  });

  it("does not allocate a port for an internal-only run", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-noop", "sleep 1");
    const started = await start(NS, "service:\n  name: internal\n");
    expect(started.exposable).toBe(false);
    expect(started.port).toBeNull();
  });

  it("ignores sync when nothing is running", async () => {
    delete process.env.OCTO_BIN_PATH;
    const result = await sync(NS, "service:\n  name: x\n");
    expect(result.running).toBe(false);
  });
});
