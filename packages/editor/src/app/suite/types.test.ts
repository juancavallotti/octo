import { describe, expect, it } from "vitest";
import {
  isEmptyExpect,
  isSharedInput,
  isValidDuration,
  mockOutcomeOf,
  outcomeOf,
} from "./types";

describe("isValidDuration", () => {
  // The value goes to Go verbatim, so the form checks the spelling here rather than
  // letting dolphin refuse the whole file over one field.
  it("accepts what time.ParseDuration accepts", () => {
    for (const ok of ["30s", "1m", "1m30s", "500ms", "1.5h", "100ns", "2us", "2µs", "0s", "-5s"]) {
      expect(isValidDuration(ok), ok).toBe(true);
    }
  });

  it("rejects what it does not", () => {
    for (const bad of ["30", "30 s", "30 seconds", "s", "", "quick", "1d", "m30s"]) {
      expect(isValidDuration(bad), JSON.stringify(bad)).toBe(false);
    }
  });
});

describe("outcomeOf", () => {
  // Three-way, never three independent fields: dolphin rejects the combinations, so a
  // form offering them as checkboxes is offering the user a file that will not load.
  it("names the one outcome an expectation states", () => {
    expect(outcomeOf(undefined)).toBe("message");
    expect(outcomeOf({})).toBe("message");
    expect(outcomeOf({ body: { x: 1 } })).toBe("message");
    expect(outcomeOf({ dropped: true })).toBe("dropped");
    expect(outcomeOf({ error: "boom" })).toBe("error");
    // An invalid combination still resolves to something the form can render, so the
    // user can see the case and fix it; validate.ts is what reports it.
    expect(outcomeOf({ dropped: true, error: "boom" })).toBe("error");
  });
});

describe("isEmptyExpect", () => {
  it("is true only when nothing at all is asserted", () => {
    expect(isEmptyExpect(undefined)).toBe(true);
    expect(isEmptyExpect({})).toBe(true);
    expect(isEmptyExpect({ vars: {}, that: [] })).toBe(true);
    // A null body is an assertion: the flow returned a message whose body was null.
    expect(isEmptyExpect({ body: null })).toBe(false);
    expect(isEmptyExpect({ vars: { a: 1 } })).toBe(false);
    expect(isEmptyExpect({ that: ["true"] })).toBe(false);
  });
});

describe("isSharedInput", () => {
  it("tells a reference from an inline input", () => {
    expect(isSharedInput("vip")).toBe(true);
    expect(isSharedInput({ data: 1 })).toBe(false);
    expect(isSharedInput(undefined)).toBe(false);
  });
});

describe("mockOutcomeOf", () => {
  it("reads the one thing a mocked block does", () => {
    expect(mockOutcomeOf({ when: "x", body: { ok: true } })).toBe("body");
    expect(mockOutcomeOf({ when: "x", error: "boom" })).toBe("error");
    expect(mockOutcomeOf({ when: "x", drop: true })).toBe("drop");
  });

  // A case that has not said yet is on its way to returning a message, which is what
  // almost every mock is for — so `body` is the fall-through, not a fourth state.
  it("falls through to body when nothing has been said", () => {
    expect(mockOutcomeOf(undefined)).toBe("body");
    expect(mockOutcomeOf({})).toBe("body");
  });
});
