import net from "node:net";
import { afterEach, describe, expect, it } from "vitest";
import { available, choosePort, pinnedPort, PREFERRED_PORT } from "./port";

/**
 * The port logic is worth testing precisely because its failure mode is invisible:
 * a fallback that silently landed on an ephemeral port would work perfectly on the
 * developer's machine and quietly break every agent's MCP config on the user's.
 */

const held: net.Server[] = [];

/** Occupy a port for the duration of a test. */
function hold(port: number): Promise<void> {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.once("error", reject);
    server.listen(port, "127.0.0.1", () => {
      held.push(server);
      resolve();
    });
  });
}

afterEach(async () => {
  await Promise.all(held.splice(0).map((s) => new Promise((r) => s.close(r))));
});

describe("available", () => {
  it("is false for a port something is listening on", async () => {
    await hold(PREFERRED_PORT);
    expect(await available(PREFERRED_PORT)).toBe(false);
  });

  it("is true again once the holder lets go", async () => {
    await hold(PREFERRED_PORT);
    await Promise.all(held.splice(0).map((s) => new Promise((r) => s.close(r))));
    expect(await available(PREFERRED_PORT)).toBe(true);
  });
});

describe("choosePort", () => {
  it("prefers the deterministic port, so the MCP URL is stable across launches", async () => {
    expect(await choosePort()).toBe(PREFERRED_PORT);
    expect(await choosePort()).toBe(PREFERRED_PORT);
  });

  it("walks up to the next free port when the preferred one is taken", async () => {
    await hold(PREFERRED_PORT);
    expect(await choosePort()).toBe(PREFERRED_PORT + 1);
  });

  it("skips a run of taken ports", async () => {
    await hold(PREFERRED_PORT);
    await hold(PREFERRED_PORT + 1);
    await hold(PREFERRED_PORT + 2);
    expect(await choosePort()).toBe(PREFERRED_PORT + 3);
  });

  it("falls back to 0 — let the OS choose — rather than refusing to start", async () => {
    // A range of one, fully occupied: the exhausted case without binding 20 ports.
    await hold(PREFERRED_PORT);
    expect(await choosePort(PREFERRED_PORT, 1)).toBe(0);
  });
});

describe("pinnedPort", () => {
  it("reads a valid override", () => {
    expect(pinnedPort({ OCTO_DESKTOP_PORT: "9001" })).toBe(9001);
  });

  it("ignores nonsense rather than starting on a port nobody meant", () => {
    for (const OCTO_DESKTOP_PORT of ["", "nope", "0", "-1", "70000", "80.5"]) {
      expect(pinnedPort({ OCTO_DESKTOP_PORT })).toBeNull();
    }
  });

  it("is null when unset", () => {
    expect(pinnedPort({})).toBeNull();
  });
});
