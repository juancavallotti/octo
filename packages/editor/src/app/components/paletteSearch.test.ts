import { describe, expect, it } from "vitest";
import { Box } from "lucide-react";
import type { PaletteComponent } from "./palette";
import { rank, subsequence } from "./paletteSearch";

const item = (id: string, label: string, group = "Core"): PaletteComponent => ({
  id,
  label,
  icon: Box,
  group,
});

const items = [
  item("log", "Log"),
  item("ai-agent", "AI Agent"),
  item("ai-router", "AI Router"),
  item("rest", "REST Call"),
  item("set-variable", "Set Variable"),
];

const labels = (query: string) => rank(items, query).map((i) => i.label);

describe("subsequence", () => {
  it("matches characters in order with gaps", () => {
    expect(subsequence("AI Agent", "aag")).toEqual([0, 3, 4]);
  });

  it("does not match out of order", () => {
    expect(subsequence("AI Agent", "ga")).toBeNull();
  });

  it("does not match what is not there", () => {
    expect(subsequence("Log", "logg")).toBeNull();
  });
});

describe("rank", () => {
  it("keeps schema order when nothing is typed", () => {
    expect(labels("")).toEqual(items.map((i) => i.label));
  });

  it("puts an exact label first", () => {
    expect(labels("log")[0]).toBe("Log");
  });

  it("finds a block by its runtime type even when the label reads differently", () => {
    expect(labels("set-variable")).toContain("Set Variable");
  });

  it("prefers a prefix over a match in the middle", () => {
    // "re" is a prefix of "REST Call" and buried inside "Set Variable".
    expect(labels("re")[0]).toBe("REST Call");
  });

  it("finds a hyphenated type from a partial", () => {
    expect(labels("ai ag")[0]).toBe("AI Agent");
  });

  it("drops what does not match at all", () => {
    expect(labels("zzz")).toEqual([]);
  });

  it("reports where it matched, so the row can highlight it", () => {
    expect(rank(items, "log")[0].matched).toEqual([0, 1, 2]);
  });
});
