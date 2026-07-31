// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { chmod, mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { cachedTestVersion, cachedVersion, probeTestVersion, probeVersion } from "./version";

/** Writes an executable shell script standing in for a real binary. */
async function fakeBin(dir: string, name: string, body: string): Promise<string> {
  const path = join(dir, name);
  await writeFile(path, `#!/bin/sh\n${body}\n`, "utf8");
  await chmod(path, 0o755);
  return path;
}

/**
 * The caches live on globalThis so they survive Next's HMR, which means they also
 * survive between tests — so each one clears them first. Without this a test would
 * silently assert on the previous test's probe.
 */
function clearCaches() {
  const store = globalThis as unknown as Record<string, unknown>;
  delete store.__octoRuntimeVersion;
  delete store.__octoTestVersion;
}

describe("version probes", () => {
  let dir: string;

  beforeEach(async () => {
    dir = await mkdtemp(join(tmpdir(), "octo-version-"));
    clearCaches();
  });

  afterEach(() => {
    delete process.env.OCTO_BIN_PATH;
    delete process.env.DOLPHIN_BIN_PATH;
    clearCaches();
  });

  it("reads dolphin's version line and caches it", async () => {
    process.env.DOLPHIN_BIN_PATH = await fakeBin(
      dir,
      "dolphin-version",
      'echo "dolphin 1.2.3 (built today)"',
    );

    expect(await probeTestVersion()).toBe("dolphin 1.2.3 (built today)");
    expect(cachedTestVersion()).toBe("dolphin 1.2.3 (built today)");
  });

  it("asks dolphin with its own spelling of the subcommand", async () => {
    // `dolphin version`, not `--version`: it accepts both, but a fake that echoes
    // its argv proves we send the one dolphin's own help documents.
    process.env.DOLPHIN_BIN_PATH = await fakeBin(dir, "dolphin-argv", 'echo "$@"');
    expect(await probeTestVersion()).toBe("version");
  });

  it("reports null rather than throwing when dolphin is not configured", async () => {
    delete process.env.DOLPHIN_BIN_PATH;
    expect(await probeTestVersion()).toBeNull();
    expect(cachedTestVersion()).toBeNull();
  });

  it("reports null when dolphin will not run", async () => {
    process.env.DOLPHIN_BIN_PATH = await fakeBin(dir, "dolphin-broken", "exit 1");
    expect(await probeTestVersion()).toBeNull();
  });

  // The two probes are in one module and must not share a cache: a host with one
  // binary and not the other has to report exactly that.
  it("caches the two binaries independently", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-version", 'echo "octo 9.9.9"');
    delete process.env.DOLPHIN_BIN_PATH;

    expect(await probeVersion()).toBe("octo 9.9.9");
    expect(await probeTestVersion()).toBeNull();
    expect(cachedVersion()).toBe("octo 9.9.9");
    expect(cachedTestVersion()).toBeNull();
  });
});
