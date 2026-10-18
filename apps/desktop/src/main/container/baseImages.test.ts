import { describe, expect, it, vi } from "vitest";
import { compareTags, hubTags, mergeTags, releaseOf } from "./baseImages";

// Hoisted above the import: settings reaches Electron, which a unit test does not have.
vi.mock("../settings", () => ({ binary: () => "", probeVersion: async () => null }));

describe("compareTags", () => {
  it("puts latest first and releases newest first, numerically", () => {
    const tags = ["0.9.0", "nightly", "0.11.8", "latest", "0.11.10", "0.10.2"];
    expect(tags.sort(compareTags)).toEqual([
      "latest",
      "0.11.10",
      "0.11.8",
      "0.10.2",
      "0.9.0",
      "nightly",
    ]);
  });
});

describe("mergeTags", () => {
  it("marks a published tag that is also pulled, and keeps local-only ones", () => {
    expect(mergeTags(["latest", "0.11.9"], ["0.11.9", "mine"])).toEqual([
      { tag: "latest", remote: true, local: false },
      { tag: "0.11.9", remote: true, local: true },
      { tag: "mine", remote: false, local: true },
    ]);
  });
});

describe("hubTags", () => {
  it("follows pages up to the bound", async () => {
    const pages = [
      { results: [{ name: "latest" }, { name: "0.11.9" }], next: "p2" },
      { results: [{ name: "0.11.8" }], next: "p3" },
      { results: [{ name: "0.11.7" }], next: null },
    ];
    let i = 0;
    const fetchFn = async () => ({ ok: true, json: async () => pages[i++] });
    expect(await hubTags(fetchFn, 2)).toEqual(["latest", "0.11.9", "0.11.8"]);
  });

  it("fails when Docker Hub refuses, so the caller can say it is offline", async () => {
    const fetchFn = async () => ({ ok: false, json: async () => ({}) });
    await expect(hubTags(fetchFn)).rejects.toThrow();
  });
});

describe("releaseOf", () => {
  it("reads the release out of octo's version line", () => {
    expect(releaseOf("octo 0.11.9 (built 2026-10-18T18:04:31Z)")).toBe("0.11.9");
    expect(releaseOf("octo dev")).toBeNull();
    expect(releaseOf(null)).toBeNull();
  });
});
