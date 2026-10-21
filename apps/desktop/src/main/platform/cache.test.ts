import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { cachedRows, storeRows } from "./cache";

const dirs: string[] = [];
function scratch(): string {
  const dir = mkdtempSync(path.join(tmpdir(), "octo-cache-"));
  dirs.push(dir);
  return dir;
}
afterEach(() => {
  for (const dir of dirs.splice(0)) rmSync(dir, { recursive: true, force: true });
});

const rows = [{ id: "int_1", name: "billing", lastUpdated: "2026-10-20T10:00:00Z" }];

describe("integrations cache", () => {
  it("keeps each installation's list apart", () => {
    const dir = scratch();
    storeRows(dir, "prod", "https://api.example.com", rows);
    storeRows(dir, "dev", "http://localhost:8080", []);
    expect(cachedRows(dir, "prod", "https://api.example.com")).toEqual(rows);
    expect(cachedRows(dir, "dev", "http://localhost:8080")).toEqual([]);
    expect(cachedRows(dir, "staging", "https://staging.example.com")).toBeNull();
  });

  it("forgets the list of a name now pointed at another platform", () => {
    const dir = scratch();
    storeRows(dir, "prod", "https://api.example.com", rows);
    expect(cachedRows(dir, "prod", "https://other.example.com")).toBeNull();
  });
});
