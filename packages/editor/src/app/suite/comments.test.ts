import { describe, it, expect } from "vitest";
import { hasComments } from "./comments";
import { serializeSuite } from "./serialize";
import { parseSuite } from "./parse";
import { scaffoldSuite } from "../components/testing/scaffold";

describe("hasComments", () => {
  it("finds a comment on its own line", () => {
    expect(hasComments("# the happy path\nflow: orders\ncases: []\n")).toBe(true);
  });

  it("finds one at the end of a line", () => {
    expect(hasComments("flow: orders # the one under test\ncases: []\n")).toBe(true);
  });

  it("finds one nested inside a case", () => {
    expect(
      hasComments("flow: orders\ncases:\n  - name: it runs\n    # why it matters\n    skip: soon\n"),
    ).toBe(true);
  });

  it("finds one in a file that is nothing else", () => {
    expect(hasComments("# nothing here yet\n")).toBe(true);
  });

  it("says no when there are none", () => {
    expect(hasComments("flow: orders\ncases:\n  - name: it runs\n")).toBe(false);
  });

  // A `#` inside a quoted value is not a comment, and warning about it would train the
  // user to ignore the banner.
  it("does not mistake a hash inside a string for one", () => {
    expect(hasComments('flow: orders\ncases:\n  - name: "issue #12"\n')).toBe(false);
  });

  it("says no for unreadable YAML, which the form is disabled for anyway", () => {
    expect(hasComments("flow: [unclosed\n")).toBe(false);
  });
});

describe("the banner's premise", () => {
  // The banner claims the form drops comments. If serializing ever started keeping them
  // the banner would be a lie, so the claim is pinned to the behaviour rather than left
  // as prose.
  it("holds: a scaffolded suite has comments and loses them when re-serialized", () => {
    const scaffold = scaffoldSuite("orders");
    expect(hasComments(scaffold)).toBe(true);

    const round = serializeSuite(parseSuite(scaffold).suite);
    expect(hasComments(round)).toBe(false);
  });
});
