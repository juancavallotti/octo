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
  it("reports the configured root and its folder name", async () => {
    process.env.OCTO_FS_DIR = "/tmp/octo-vault-test/orders";
    const res = await GET();
    expect(res.status).toBe(200);
    await expect(res.json()).resolves.toEqual({
      path: "/tmp/octo-vault-test/orders",
      name: "orders",
    });
  });

  it("follows a changed root without a restart", async () => {
    process.env.OCTO_FS_DIR = "/tmp/octo-vault-test/one";
    await expect((await GET()).json()).resolves.toMatchObject({ name: "one" });
    process.env.OCTO_FS_DIR = "/tmp/octo-vault-test/two";
    await expect((await GET()).json()).resolves.toMatchObject({ name: "two" });
  });

  it("falls back to the store's default root when unset", async () => {
    delete process.env.OCTO_FS_DIR;
    const body = (await (await GET()).json()) as { path: string; name: string };
    // Whatever fsRoot() defaults to, the two fields must agree — the name is the
    // basename of the path, not a separately-derived answer.
    expect(body.name).toBe("flows");
    expect(body.path.endsWith("/flows")).toBe(true);
  });
});
