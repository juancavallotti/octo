import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { folderDigest, replaceContents, syncState } from "./sync";

const dirs: string[] = [];
function folder(files: Record<string, string>): string {
  const dir = mkdtempSync(path.join(tmpdir(), "octo-sync-"));
  dirs.push(dir);
  for (const [name, content] of Object.entries(files)) {
    mkdirSync(path.dirname(path.join(dir, name)), { recursive: true });
    writeFileSync(path.join(dir, name), content);
  }
  return dir;
}
afterEach(() => {
  for (const dir of dirs.splice(0)) rmSync(dir, { recursive: true, force: true });
});

describe("folderDigest", () => {
  it("follows what a push would send and nothing else", () => {
    const base = folderDigest(folder({ "flow.yaml": "a", "res/db.yaml": "b" }));
    expect(folderDigest(folder({ "flow.yaml": "a", "res/db.yaml": "b" }))).toBe(base);
    // Hidden entries stay behind on a push, the link among them.
    expect(folderDigest(folder({ "flow.yaml": "a", "res/db.yaml": "b", ".octo/platform.json": "{}", ".env": "x" }))).toBe(base);

    expect(folderDigest(folder({ "flow.yaml": "changed", "res/db.yaml": "b" }))).not.toBe(base);
    expect(folderDigest(folder({ "flow.yaml": "a" }))).not.toBe(base);
    expect(folderDigest(folder({ "flow.yaml": "a", "res/other.yaml": "b" }))).not.toBe(base);
  });
});

describe("syncState", () => {
  const last = { digest: "d1", lastUpdated: "2026-10-20T10:00:00Z" };

  it("compares each side with itself at the last sync", () => {
    expect(syncState(last, "d1", "2026-10-20T10:00:00Z")).toBe("synced");
    expect(syncState(last, "d2", "2026-10-20T10:00:00Z")).toBe("ahead");
    expect(syncState(last, "d1", "2026-10-20T11:00:00Z")).toBe("behind");
    expect(syncState(last, "d2", "2026-10-20T11:00:00Z")).toBe("diverged");
  });

  it("vouches for neither side of a folder that never recorded a sync", () => {
    expect(syncState(undefined, "d1", "2026-10-20T10:00:00Z")).toBe("diverged");
  });
});

describe("replaceContents", () => {
  it("holds what was pulled, keeping the folder's own hidden entries", () => {
    const dir = folder({
      "flow.yaml": "mine",
      "deleted-upstream.yaml": "x",
      ".env": "SECRET=1",
      ".octo/platform.json": "{}",
      ".octo/octo-bundle.json": "old",
    });
    const pulled = folder({ "flow.yaml": "theirs", "res/db.yaml": "b", ".octo/octo-bundle.json": "new" });
    replaceContents(pulled, dir);

    expect(folderDigest(dir)).toBe(folderDigest(pulled));
    expect(existsSync(path.join(dir, "deleted-upstream.yaml"))).toBe(false);
    expect(readFileSync(path.join(dir, ".env"), "utf8")).toBe("SECRET=1");
    expect(existsSync(path.join(dir, ".octo/platform.json"))).toBe(true);
    expect(readFileSync(path.join(dir, ".octo/octo-bundle.json"), "utf8")).toBe("new");
  });
});
