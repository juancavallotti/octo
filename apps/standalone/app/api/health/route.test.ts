// @vitest-environment node
import { describe, expect, it, vi } from "vitest";

// Node builtins cannot be spied on in place under ESM, so stand the two spawn entry
// points up as mocks for the whole file. Nothing here should reach them.
vi.mock("node:child_process", () => ({
  spawn: vi.fn(),
  execFile: vi.fn(),
}));
import { GET } from "./route";

/**
 * The health route's whole value is that it is cheap and unconditional: the desktop
 * shell polls it while the window is still a splash screen, and treats a non-OK
 * answer as "the server never came up". So the two things worth pinning are that it
 * answers OK with nothing configured, and that it does not become a slow route by
 * accident — a future edit that made it touch the runner would be invisible in
 * manual testing and only show up as a desktop app that takes 30s to launch on a
 * machine without a runner.
 */
describe("GET /api/health", () => {
  it("answers ok", async () => {
    const res = await GET();
    expect(res.status).toBe(200);
    await expect(res.json()).resolves.toEqual({ ok: true });
  });

  it("answers with no OCTO_* environment at all", async () => {
    const saved = { ...process.env };
    delete process.env.OCTO_BIN_PATH;
    delete process.env.DOLPHIN_BIN_PATH;
    delete process.env.OCTO_FS_DIR;
    try {
      await expect((await GET()).json()).resolves.toEqual({ ok: true });
    } finally {
      process.env = saved;
    }
  });

  it("spawns nothing", async () => {
    // The probe runs on a loop; a child process per poll would be a real cost, and
    // is exactly what probing `/` would have done via probeSchema().
    const { spawn, execFile } = await import("node:child_process");
    await GET();
    expect(spawn).not.toHaveBeenCalled();
    expect(execFile).not.toHaveBeenCalled();
  });

  it("is not cached", async () => {
    // A cached health check is a lie about the present.
    expect((await GET()).headers.get("cache-control")).toBe("no-store");
  });
});
