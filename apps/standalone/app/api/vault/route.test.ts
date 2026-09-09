// @vitest-environment node
import { afterEach, describe, expect, it } from "vitest";
import { GET } from "./route";

/**
 * The vault route reports the store root, and the one behaviour worth pinning is
 * that it reads OCTO_FS_DIR *per request* rather than at import time. The desktop
 * shell restarts the whole server to switch vaults, so a cached root would be
 * correct there by accident — but the Docker image and `task dev` both set the
 * variable before the process starts, and a reader tempted to hoist `fsRoot()` into
 * a module constant would break the one deployment that changes it.
 */
const saved = process.env.OCTO_FS_DIR;

afterEach(() => {
  if (saved === undefined) delete process.env.OCTO_FS_DIR;
  else process.env.OCTO_FS_DIR = saved;
});

describe("GET /api/vault", () => {
  it("never returns the absolute path", async () => {
    // The Docker image binds 0.0.0.0 with no auth, so anything this returns is
    // readable by anyone who can reach the editor — and the path carries the
    // user's account name and directory layout.
    process.env.OCTO_FS_DIR = "/Users/someone/private/flows";
    const body = await (await GET()).json();
    expect(JSON.stringify(body)).not.toContain("/Users/someone");
    expect(Object.keys(body)).toEqual(["name"]);
  });

  it("reports the configured root and its folder name", async () => {
    process.env.OCTO_FS_DIR = "/tmp/octo-vault-test/orders";
    const res = await GET();
    expect(res.status).toBe(200);
    await expect(res.json()).resolves.toEqual({ name: "orders" });
  });

  it("follows a changed root without a restart", async () => {
    process.env.OCTO_FS_DIR = "/tmp/octo-vault-test/one";
    await expect((await GET()).json()).resolves.toMatchObject({ name: "one" });
    process.env.OCTO_FS_DIR = "/tmp/octo-vault-test/two";
    await expect((await GET()).json()).resolves.toMatchObject({ name: "two" });
  });

  it("still names a filesystem root, which has no basename", async () => {
    // path.basename("/") is "", which would render as a nameless chip.
    process.env.OCTO_FS_DIR = "/";
    await expect((await GET()).json()).resolves.toEqual({ name: "/" });
  });

  it("falls back to the store's default root when unset", async () => {
    delete process.env.OCTO_FS_DIR;
    const body = (await (await GET()).json()) as { name: string };
    expect(body.name).toBe("flows");
  });
});
