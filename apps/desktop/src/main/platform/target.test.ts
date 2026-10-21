import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { defaultTarget, folderName, isFree } from "./target";

const dirs: string[] = [];
function scratch(): string {
  const dir = mkdtempSync(path.join(tmpdir(), "octo-target-"));
  dirs.push(dir);
  return dir;
}
afterEach(() => {
  for (const dir of dirs.splice(0)) rmSync(dir, { recursive: true, force: true });
});

describe("folderName", () => {
  it("makes one folder of any name", () => {
    expect(folderName("billing/eu")).toBe("billing-eu");
    expect(folderName("..secret")).toBe("secret");
    expect(folderName("  ")).toBe("integration");
  });
});

describe("defaultTarget", () => {
  it("is the integration's name in the Projects folder", () => {
    const root = scratch();
    expect(defaultTarget(root, "billing", "prod")).toBe(path.join(root, "billing"));
  });

  it("takes an empty folder of that name, but not one with files in it", () => {
    const root = scratch();
    mkdirSync(path.join(root, "billing"));
    expect(defaultTarget(root, "billing", "prod")).toBe(path.join(root, "billing"));
    writeFileSync(path.join(root, "billing", "octo.yaml"), "");
    expect(defaultTarget(root, "billing", "prod")).toBe(path.join(root, "billing-prod"));
  });

  it("numbers it when the profile's name is taken too", () => {
    const taken = new Set(["/p/billing", "/p/billing-prod", "/p/billing-prod-2"]);
    expect(defaultTarget("/p", "billing", "prod", (d) => !taken.has(d))).toBe("/p/billing-prod-3");
  });
});

describe("isFree", () => {
  it("is a folder not there yet", () => {
    expect(isFree(path.join(scratch(), "nope"))).toBe(true);
  });
});
