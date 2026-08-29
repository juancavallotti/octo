import { describe, expect, it } from "vitest";
import { filterRanked, rankSearchString } from "./search";

describe("rankSearchString", () => {
  it("scores a tightly packed subsequence as a perfect match", () => {
    expect(rankSearchString("par", "asparagus")).toBe(1);
  });

  it("scores a prefix as a perfect match", () => {
    expect(rankSearchString("asp", "asparagus")).toBe(1);
  });

  it("costs a letter the target does not have", () => {
    // 0.6, where the original scored this 0.75 — the same as "pra", which has no
    // wrong letter at all. A typo has to cost something or the ranking cannot
    // order a clean query above a mistyped one.
    expect(rankSearchString("pora", "asparagus")).toBe(0.6);
    expect(rankSearchString("pra", "asparagus")).toBeGreaterThan(
      rankSearchString("pora", "asparagus"),
    );
  });

  it("costs a letter the query skipped", () => {
    expect(rankSearchString("prag", "asparagus")).toBe(0.8);
  });

  it("costs the distance letters had to be spread over", () => {
    expect(rankSearchString("pagus", "asparagus")).toBeCloseTo(0.714);
  });

  it("ignores case on both sides", () => {
    expect(rankSearchString("Pagus", "asparagus")).toBeCloseTo(0.714);
    expect(rankSearchString("pagus", "ASPARAGUS")).toBeCloseTo(0.714);
  });

  it("scores a sparse match low rather than dropping it", () => {
    expect(rankSearchString("pg", "asparagus")).toBeCloseTo(0.4);
  });

  it("scores something unrelated at zero", () => {
    expect(rankSearchString("o", "asparagus")).toBeCloseTo(0);
  });

  it("scores an empty query at zero", () => {
    expect(rankSearchString("", "asparagus")).toBe(0);
  });

  it("never exceeds 1, even when the query is longer than the target", () => {
    expect(rankSearchString("lemon curd", "lem", "favor-closer-length")).toBeLessThan(1);
  });

  describe("the length bias", () => {
    it("prefers the name the query accounted for more of", () => {
      const exact = rankSearchString("lemon", "lemon", "favor-closer-length");
      const longer = rankSearchString("lemon", "lemonade", "favor-closer-length");
      expect(exact).toBeGreaterThan(longer);
    });

    it("still pays nothing for a match that never happened", () => {
      // The ratio alone would pay out for a query and a target that merely
      // happen to be the same length, and 0 has to keep meaning what it says
      // for a caller's threshold to mean anything.
      expect(rankSearchString("z", "a", "favor-closer-length")).toBe(0);
      expect(rankSearchString("zzz", "abc", "favor-closer-length")).toBe(0);
    });

    it("leaves the unbiased score alone", () => {
      expect(rankSearchString("lemon", "lemon")).toBe(
        rankSearchString("lemon", "lemonade"),
      );
    });
  });

  describe("repeated letters in the target", () => {
    it("takes the tightest run, not the first one", () => {
      // Greedy from the left takes the 'a' at 0 and the 'b' at 2 and calls that
      // a gap; the pair at 1 and 2 is adjacent. A name with a doubled letter
      // should not be demoted for having one.
      expect(rankSearchString("ab", "aab")).toBe(1);
    });

    it("still scores a match its first letter cannot start", () => {
      // "bling" reaches "inventory" through its 'i' and 'n' — there is no 'b' to
      // enumerate start positions from, and that match has to survive anyway.
      expect(rankSearchString("bling", "inventory")).toBeGreaterThan(0);
    });
  });

  // The two defects the original carried, pinned so a future tidy-up cannot
  // reintroduce them.
  describe("letters are consumed, not reused", () => {
    it("does not let a repeated query letter match one target letter twice", () => {
      // "cat" holds a single 'a'. Matching it twice scored this a perfect match.
      expect(rankSearchString("aa", "cat")).toBeLessThan(1);
    });

    it("does not let a failed final letter distort the span", () => {
      // The last lookup fails; the span must be measured from the last letter
      // that actually matched, not from -1.
      expect(rankSearchString("caz", "cat")).toBeGreaterThan(0);
      expect(rankSearchString("caz", "cat")).toBeLessThan(1);
    });
  });
});

describe("filterRanked", () => {
  const apps = ["orders", "orders-reconciliation-worker", "billing", "inventory"];

  it("returns everything, in order, for an empty query", () => {
    expect(filterRanked(apps, "  ", (a) => a)).toEqual(apps);
  });

  it("ranks the closest name first", () => {
    expect(filterRanked(apps, "ord", (a) => a)[0]).toBe("orders");
  });

  it("still finds a name the query has a typo in", () => {
    expect(filterRanked(apps, "bling", (a) => a)).toContain("billing");
  });

  it("drops what does not match at all", () => {
    expect(filterRanked(apps, "zzzz", (a) => a)).toEqual([]);
  });

  it("honours the limit", () => {
    expect(filterRanked(apps, "or", (a) => a, { limit: 1 })).toHaveLength(1);
  });

  it("keeps the caller's order between equally good matches", () => {
    const tied = ["alpha-x", "alpha-y"];
    expect(filterRanked(tied, "alpha", (a) => a)).toEqual(tied);
  });
});
