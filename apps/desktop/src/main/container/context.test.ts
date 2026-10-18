import { mkdirSync, mkdtempSync, readFileSync, readdirSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { contextFiles, dockerfile, excluded, writeContext } from "./context";

/**
 * What lands in the image is what gets pushed, so the failure that matters is a file
 * that should have stayed on this machine and did not.
 */

function folder(files: Record<string, string>): string {
  const dir = mkdtempSync(path.join(tmpdir(), "octo-context-"));
  for (const [rel, content] of Object.entries(files)) {
    const file = path.join(dir, rel);
    mkdirSync(path.dirname(file), { recursive: true });
    writeFileSync(file, content);
  }
  return dir;
}

describe("excluded", () => {
  it("keeps credentials, editor metadata and test suites out", () => {
    for (const rel of [".env.dev", ".env", ".octo/meta.json", "orders_test.yaml", "a/.DS_Store"]) {
      expect(excluded(rel), rel).toBe(true);
    }
  });

  it("keeps integration files and declared resources in", () => {
    for (const rel of ["orders.yaml", "templates/welcome.tmpl", ".env.prod", "sub/.env.dev"]) {
      expect(excluded(rel), rel).toBe(false);
    }
  });
});

describe("contextFiles", () => {
  it("lists the folder minus what stays behind", async () => {
    const dir = folder({
      "orders.yaml": "flows: {}",
      "orders_test.yaml": "suite: {}",
      ".env.dev": "TOKEN=secret",
      ".octo/editor.json": "{}",
      "templates/welcome.tmpl": "hi",
    });
    expect(await contextFiles(dir)).toEqual(["orders.yaml", "templates/welcome.tmpl"]);
  });

  it("follows a symlink inside the folder and drops one that leaves it", async () => {
    const outside = folder({ "id_rsa": "private" });
    const dir = folder({ "orders.yaml": "flows: {}", "shared/a.tmpl": "a" });
    symlinkSync(path.join(outside, "id_rsa"), path.join(dir, "key.tmpl"));
    symlinkSync(path.join(dir, "shared", "a.tmpl"), path.join(dir, "alias.tmpl"));
    expect(await contextFiles(dir)).toEqual(["alias.tmpl", "orders.yaml", "shared/a.tmpl"]);
  });
});

describe("writeContext", () => {
  it("writes the files with a Dockerfile on the chosen runtime", async () => {
    const dir = folder({ "orders.yaml": "flows: {}", ".env.dev": "TOKEN=secret" });
    const dest = path.join(mkdtempSync(path.join(tmpdir(), "octo-dest-")), "ctx");
    await writeContext(dir, dest, "0.11.8");
    expect(readdirSync(dest).sort()).toEqual([".dockerignore", "Dockerfile", "orders.yaml"]);
    expect(readFileSync(path.join(dest, "Dockerfile"), "utf8")).toBe(dockerfile("0.11.8"));
    expect(dockerfile("0.11.8")).toContain("FROM juancavallotti/octo-runtime:0.11.8\n");
  });

  it("refuses a destination that already has something in it", async () => {
    const dir = folder({ "orders.yaml": "flows: {}" });
    const dest = folder({ "keep.txt": "mine" });
    await expect(writeContext(dir, dest, "latest")).rejects.toThrow(/not empty/);
  });

  it("refuses a folder with no integration in it", async () => {
    const dir = folder({ "templates/a.tmpl": "a" });
    const dest = path.join(mkdtempSync(path.join(tmpdir(), "octo-dest-")), "ctx");
    await expect(writeContext(dir, dest, "latest")).rejects.toThrow(/no integration/);
  });
});
