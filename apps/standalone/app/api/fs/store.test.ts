import { afterAll, beforeEach, describe, expect, it } from "vitest";
import { mkdtemp, rm, readdir, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import {
  isStoredFile,
  listConfigFiles,
  nameOf,
  projectId,
  readProject,
  writeProject,
} from "./store";

// A real temp directory backs the store (OCTO_FS_DIR); each test starts clean.
let dir: string;

beforeEach(async () => {
  dir = await mkdtemp(path.join(tmpdir(), "octo-fs-"));
  process.env.OCTO_FS_DIR = dir;
});

afterAll(async () => {
  if (dir) await rm(dir, { recursive: true, force: true });
});

describe("standalone project store", () => {
  it("is the folder: its id is the folder's name, its files the config files in it", async () => {
    await writeFile(path.join(dir, "orders.yaml"), "flows: []\n");
    await writeFile(path.join(dir, "octo.yaml"), "service:\n  name: shop\n");
    await writeFile(path.join(dir, "ignore.txt"), "");
    const project = await readProject();
    expect(project.id).toBe(path.basename(dir));
    expect(project.id).toBe(projectId());
    expect(project.files.map((f) => f.path)).toEqual(["octo.yaml", "orders.yaml"]);
  });

  it("is named by the service its files declare, else by its folder", async () => {
    expect((await readProject()).name).toBe(path.basename(dir));
    await writeFile(path.join(dir, "octo.yaml"), "service:\n  name: shop\n");
    expect((await readProject()).name).toBe("shop");
  });

  it("writes every file and removes the config files no longer given", async () => {
    await writeProject([
      { path: "octo.yaml", content: "service:\n  name: shop\n" },
      { path: "orders.yaml", content: "flows: []\n" },
    ]);
    await writeProject([
      { path: "octo.yaml", content: "service:\n  name: shop\n" },
      { path: "billing.yaml", content: "flows: []\n" },
    ]);
    expect((await readdir(dir)).sort()).toEqual(["billing.yaml", "octo.yaml"]);
  });

  // A suite lives beside the flow it tests, exactly as octo's own directory loader
  // expects (runtime/core/runtime/config.go skips these). It is not a config file, so
  // it is neither listed nor removed by a write that does not mention it.
  it("leaves test suites and other files alone", async () => {
    await writeFile(path.join(dir, "orders_test.yaml"), "flow: orders\n");
    await writeFile(path.join(dir, "notes.txt"), "keep");
    await writeProject([{ path: "orders.yaml", content: "flows: []\n" }]);
    expect(await listConfigFiles()).toEqual(["orders.yaml"]);
    expect(await readFile(path.join(dir, "orders_test.yaml"), "utf8")).toBe("flow: orders\n");
    expect(await readFile(path.join(dir, "notes.txt"), "utf8")).toBe("keep");
  });

  it("refuses paths that try to escape the root, and test suites", async () => {
    const attacks = [
      "../secret.yaml", // parent dir
      "../../etc/passwd.yaml", // deep traversal
      "/etc/passwd.yaml", // absolute path
      "sub/dir.yaml", // nested dir
      "a/../../b.yaml", // separators + traversal
      "..\\win.yaml", // backslash separator
      ".hidden.yaml", // leading-dot dotfile
      "x.txt", // not a yaml
      "noext", // no extension
      "orders_test.yaml", // a suite, not config
    ];
    for (const p of attacks) {
      await expect(writeProject([{ path: p, content: "x" }]), p).rejects.toThrow();
    }
    // Nothing got written outside (or inside) the root.
    expect(await readdir(dir)).toEqual([]);
  });

  it("refuses a file given twice", async () => {
    await expect(
      writeProject([
        { path: "a.yaml", content: "x" },
        { path: "a.yaml", content: "y" },
      ]),
    ).rejects.toThrow(/more than once/);
  });
});

/**
 * These two are exported so nothing else answers "is this a file we serve?" for
 * itself: a second copy of the rule that missed `.yml` would leave those flows stale.
 */
describe("filename rules shared with the watcher", () => {
  it("accepts both spellings of the extension", () => {
    expect(isStoredFile("orders.yaml")).toBe(true);
    expect(isStoredFile("orders.yml")).toBe(true);
  });

  it("rejects dotfiles, nested paths and anything else", () => {
    expect(isStoredFile(".hidden.yaml")).toBe(false);
    expect(isStoredFile("sub/orders.yaml")).toBe(false);
    expect(isStoredFile("../orders.yaml")).toBe(false);
    expect(isStoredFile("orders.json")).toBe(false);
  });

  it("strips either extension to get the name", () => {
    expect(nameOf("orders.yaml")).toBe("orders");
    expect(nameOf("orders.yml")).toBe("orders");
  });
});
