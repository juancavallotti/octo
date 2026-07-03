import { afterAll, beforeEach, describe, expect, it } from "vitest";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fsResourceProvider } from "./resources";

// A real temp directory backs the flows store (OCTO_FS_DIR); each test starts clean.
let dir: string;

beforeEach(async () => {
  dir = await mkdtemp(path.join(tmpdir(), "octo-res-"));
  process.env.OCTO_FS_DIR = dir;
});

afterAll(async () => {
  if (dir) await rm(dir, { recursive: true, force: true });
});

describe("standalone fsResourceProvider", () => {
  it("reads declared resources, including nested and dotfile names", async () => {
    await writeFile(path.join(dir, ".env.dev"), "A=1\n", "utf8");
    await mkdir(path.join(dir, "templates"), { recursive: true });
    await writeFile(path.join(dir, "templates/welcome.tmpl"), "hi", "utf8");

    const files = await fsResourceProvider([".env.dev", "templates/welcome.tmpl"]);
    expect(files).toEqual([
      { name: ".env.dev", content: "A=1\n" },
      { name: "templates/welcome.tmpl", content: "hi" },
    ]);
  });

  it("omits names with no file on disk", async () => {
    await writeFile(path.join(dir, ".env.dev"), "A=1", "utf8");
    const files = await fsResourceProvider([".env.dev", "missing.tmpl"]);
    expect(files.map((f) => f.name)).toEqual([".env.dev"]);
  });

  it("refuses to read outside the flows root", async () => {
    // A secret one level above the root must never be resolved by a traversal name.
    await writeFile(path.join(dir, "..", "secret.env"), "S=1", "utf8").catch(() => {});
    const files = await fsResourceProvider(["../secret.env"]);
    // "../secret.env" is clamped under root → resolves to <root>/secret.env, which
    // doesn't exist, so nothing is returned.
    expect(files).toEqual([]);
  });
});
