import { describe, expect, it } from "vitest";
import {
  ancestorPaths,
  blockLabel,
  isInside,
  parseBlockPath,
} from "./blockPath";

/**
 * The path grammar this parser has to agree with lives in
 * `runtime/core/blockpath.go`, and the shapes below are the ones the flow builder
 * actually mints (`runtime/core/internal/engine/flow.go`): a flow name, optionally
 * carrying its error chain, then `.label` per block, with `[branch]` on any block a
 * deeper one was reached through.
 *
 * The second half of this file is the part that matters. A block's address is
 * built from its name with no check that the name is spellable, so a hand-written
 * definition can mint a path this grammar cannot read back. Every one of those
 * cases has to produce *something* — a strange label, an odd tree — and never an
 * exception, because the alternative is a trace nobody can open.
 */

/** The shape assertions read against, so a test names only what it is about. */
function shape(path: string) {
  return parseBlockPath(path).map((s) => [s.label, s.branch, s.path]);
}

describe("parseBlockPath", () => {
  it("reads a flow and the block under it", () => {
    expect(shape("orders.api-call-1")).toEqual([
      ["orders", null, "orders"],
      ["api-call-1", null, "orders.api-call-1"],
    ]);
  });

  it("reads the branch a composite was descended through", () => {
    // The runtime's own example.
    expect(shape("orders.checkHeader[else].api-call-1")).toEqual([
      ["orders", null, "orders"],
      ["checkHeader", "else", "orders.checkHeader"],
      ["api-call-1", null, "orders.checkHeader[else].api-call-1"],
    ]);
  });

  it("reads a flow's error chain as the flow's own branch", () => {
    expect(shape("orders[error].notify")).toEqual([
      ["orders", "error", "orders"],
      ["notify", null, "orders[error].notify"],
    ]);
  });

  it("reads a fork branch addressed by position", () => {
    // MemberBranch falls back to the index when a member has no name.
    expect(shape("orders.fan[0].call")).toEqual([
      ["orders", null, "orders"],
      ["fan", "0", "orders.fan"],
      ["call", null, "orders.fan[0].call"],
    ]);
  });

  it("carries no steps for a record that names no block", () => {
    // Every flow.* and source.* record has an empty path.
    expect(parseBlockPath("")).toEqual([]);
    expect(ancestorPaths("")).toEqual([]);
    expect(blockLabel("")).toBe("");
  });

  it("nests as deep as the path goes", () => {
    const path = "orders.a[then].b[body].c[0].d";
    expect(parseBlockPath(path)).toHaveLength(5);
    expect(blockLabel(path)).toBe("d");
    expect(ancestorPaths(path)).toEqual([
      "orders",
      "orders.a",
      "orders.a[then].b",
      "orders.a[then].b[body].c",
    ]);
  });

  // --- paths the grammar cannot fully read back -----------------------------

  it("keeps a dot inside a branch name inside that branch", () => {
    // A fork branch or a switch case may be named anything; only a "." outside
    // every bracket separates two steps.
    expect(shape("orders.route[a.b].call")).toEqual([
      ["orders", null, "orders"],
      ["route", "a.b", "orders.route"],
      ["call", null, "orders.route[a.b].call"],
    ]);
  });

  it("leaves an unterminated bracket in the label", () => {
    // Nothing can say where the branch was meant to end, so nothing claims to.
    expect(shape("orders.a[then")).toEqual([
      ["orders", null, "orders"],
      ["a[then", null, "orders.a[then"],
    ]);
  });

  it("treats a stray closing bracket as part of the name", () => {
    expect(shape("orders.a]b")).toEqual([
      ["orders", null, "orders"],
      ["a]b", null, "orders.a]b"],
    ]);
  });

  it("refuses to read a branch out of a segment with trailing text", () => {
    // `a[then]x` is not `label[branch]`, so no branch is claimed from it.
    expect(shape("orders.a[then]x")).toEqual([
      ["orders", null, "orders"],
      ["a[then]x", null, "orders.a[then]x"],
    ]);
  });

  it("survives a block named with the separator", () => {
    // A block named "a.b" mints a path indistinguishable from a block "b" inside
    // a composite "a" — the ambiguity is in the runtime's address, not here. What
    // this asserts is that it parses at all, and that the wrong reading is the
    // harmless one: "orders.a" reports no span, so nothing reparents under it.
    expect(shape("orders.a.b")).toEqual([
      ["orders", null, "orders"],
      ["a", null, "orders.a"],
      ["b", null, "orders.a.b"],
    ]);
  });

  it("never throws, whatever it is handed", () => {
    const nasty = [
      "[",
      "]",
      "[]",
      ".",
      "..",
      "a..b",
      "a[",
      "a]",
      "a[[b]]",
      "a[b][c]",
      "[a].b",
      "a.",
      ".a",
      "🙂.🙂[🙂].🙂",
    ];
    for (const path of nasty) {
      expect(() => parseBlockPath(path), path).not.toThrow();
      expect(() => ancestorPaths(path), path).not.toThrow();
      expect(() => isInside("orders", path), path).not.toThrow();
      expect(() => blockLabel(path), path).not.toThrow();
    }
  });

  it("keeps an empty step rather than collapsing it away", () => {
    // "orders." is not "orders": dropping the empty step would claim the two
    // addressed the same block, which is a nesting claim nothing supports.
    expect(shape("orders.")).toEqual([
      ["orders", null, "orders"],
      ["", null, "orders."],
    ]);
  });
});

describe("isInside", () => {
  it("holds for a block reached through a branch of the outer one", () => {
    expect(isInside("orders.fan", "orders.fan[0].call")).toBe(true);
    expect(isInside("orders", "orders.fan[0].call")).toBe(true);
  });

  it("does not hold for a block that merely shares a prefix", () => {
    // The separator is what makes this a different block, not a deeper one.
    expect(isInside("orders.fan", "orders.fanout")).toBe(false);
    expect(isInside("orders.fan", "orders.fan-2[0].call")).toBe(false);
  });

  it("does not hold for a block against itself", () => {
    expect(isInside("orders.fan", "orders.fan")).toBe(false);
  });

  it("does not hold across a different branch of the same composite", () => {
    // Same block name reached through a different branch is a different block,
    // and the branch is part of the ancestor's recorded path.
    expect(isInside("orders.fan[0].call", "orders.fan[1].call.inner")).toBe(false);
  });

  it("is never satisfied by an empty outer path", () => {
    // Flow and source records carry no path at all, so "" addresses no block and
    // can enclose nothing. It has to be rejected explicitly rather than fall out
    // of the prefix walk: a path whose first step is empty — which is what an
    // unnamed flow would mint — really does have "" among its ancestors, and
    // without the guard every block in it would hang off a record that is not a
    // block.
    expect(isInside("", ".notify")).toBe(false);
    expect(isInside("", "orders.fan")).toBe(false);
    expect(isInside("", "")).toBe(false);
  });
});
