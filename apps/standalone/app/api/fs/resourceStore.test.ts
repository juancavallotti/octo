import { afterAll, beforeEach, describe, expect, it } from "vitest";
import { access, mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { listResources, readResource, writeResource } from "./resourceStore";

// A real temp directory backs the store (OCTO_FS_DIR); each test starts clean.
let dir: string;

beforeEach(async () => {
  dir = await mkdtemp(path.join(tmpdir(), "octo-res-"));
  process.env.OCTO_FS_DIR = dir;
});

afterAll(async () => {
  if (dir) await rm(dir, { recursive: true, force: true });
});

describe("standalone resource store", () => {
  it("writes and reads a dotfile resource", async () => {
    await writeResource(".env.dev", "A=1\n");
    expect(await readResource(".env.dev")).toBe("A=1\n");
    expect(await readFile(path.join(dir, ".env.dev"), "utf8")).toBe("A=1\n");
  });

  it("creates parent directories for a nested resource name", async () => {
    await writeResource("templates/welcome.tmpl", "hi");
    expect(await readResource("templates/welcome.tmpl")).toBe("hi");
  });

  it("returns null for a missing resource", async () => {
    expect(await readResource(".env.dev")).toBeNull();
  });

  it("overwrites an existing resource", async () => {
    await writeResource(".env.dev", "A=1");
    await writeResource(".env.dev", "A=2");
    expect(await readResource(".env.dev")).toBe("A=2");
  });

  it("clamps a traversal name under the root instead of escaping it", async () => {
    await writeResource("../evil", "x");
    // Written inside the root (clamped), never in the parent directory.
    expect(await readFile(path.join(dir, "evil"), "utf8")).toBe("x");
    await expect(access(path.join(dir, "..", "evil"))).rejects.toThrow();
  });

  // The flows root is shared three ways: store.ts owns the flow documents,
  // testSuiteStore.ts owns the `*_test.yaml` suites, and this store owns the rest.
  // Neither of the other two's files may show up in the Resources tab.
  it("lists neither flows nor test suites as resources", async () => {
    await writeResource("orders.yaml", "flow");
    await writeResource("orders_test.yaml", "suite");
    await writeResource(".env.dev", "A=1");
    await writeResource("templates/welcome.tmpl", "hi");

    const names = (await listResources()).map((r) => r.name);

    expect(names).toEqual([".env.dev", "templates/welcome.tmpl"]);
  });
});
