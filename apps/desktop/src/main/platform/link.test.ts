import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { linkFile, readLink, removeLink, writeLink, type Link } from "./link";

const dirs: string[] = [];
function scratch(): string {
  const dir = mkdtempSync(path.join(tmpdir(), "octo-link-"));
  dirs.push(dir);
  return dir;
}
afterEach(() => {
  for (const dir of dirs.splice(0)) rmSync(dir, { recursive: true, force: true });
});

const link: Link = {
  profile: "prod",
  url: "https://api.example.com",
  integrationId: "int_1",
  integrationName: "billing",
  pulledTag: "v1.0.0",
  linkedAt: 1,
};

describe("link", () => {
  it("round-trips", () => {
    const dir = scratch();
    writeLink(dir, link);
    expect(readLink(dir)).toEqual(link);
    removeLink(dir);
    expect(readLink(dir)).toBeNull();
  });

  it("is none for a folder that was never linked", () => {
    expect(readLink(scratch())).toBeNull();
  });

  it("is none when the file is not a link, rather than half of one", () => {
    const dir = scratch();
    mkdirSync(path.dirname(linkFile(dir)), { recursive: true });
    writeFileSync(linkFile(dir), JSON.stringify({ profile: "prod" }));
    expect(readLink(dir)).toBeNull();
    writeFileSync(linkFile(dir), "not json");
    expect(readLink(dir)).toBeNull();
  });
});
