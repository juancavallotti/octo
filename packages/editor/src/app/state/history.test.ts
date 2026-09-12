import { describe, expect, it } from "vitest";
import { DEFAULT_LIMIT, initialHistory, withHistory, type HistoryPolicy } from "./history";

/**
 * The wrapper is tested against a toy reducer rather than the editor's, because what
 * is worth pinning down here is the history algebra — coalescing, the forked future,
 * the depth cap — and none of it has anything to do with flows.
 */

interface Doc {
  text: string;
  view: string;
}
type Act =
  | { type: "type"; field: string; value: string }
  | { type: "look"; at: string }
  | { type: "load"; text: string }
  | { type: "undo" }
  | { type: "redo" }
  | { type: "noop" };

function inner(state: Doc, action: Act): Doc {
  switch (action.type) {
    case "type":
      return { ...state, text: action.value };
    case "look":
      return { ...state, view: action.at };
    case "load":
      return { text: action.text, view: state.view };
    default:
      return state;
  }
}

const policy: HistoryPolicy<Act> = {
  track: (a) => (a.type === "load" ? "reset" : a.type === "type" ? "record" : "skip"),
  isUndo: (a) => a.type === "undo",
  isRedo: (a) => a.type === "redo",
  coalesceKey: (a) => (a.type === "type" ? a.field : null),
};

const reduce = withHistory(inner, policy);
const start = initialHistory<Doc, Act>({ text: "", view: "canvas" });

/** Apply a sequence, so the tests read as the interactions they describe. */
function run(...actions: Act[]) {
  return actions.reduce(reduce, start);
}

describe("withHistory", () => {
  it("undoes a recorded edit", () => {
    const h = reduce(run({ type: "type", field: "a", value: "one" }), { type: "undo" });
    expect(h.present.text).toBe("");
  });

  it("restores the exact previous state object, so identity checks still hold", () => {
    const before = run({ type: "type", field: "a", value: "one" });
    const after = reduce(reduce(before, { type: "type", field: "b", value: "two" }), {
      type: "undo",
    });
    // The editor's save layer decides "dirty" by comparing document identity; an undo
    // that rebuilt an equal-but-different object would leave it dirty forever.
    expect(after.present).toBe(before.present);
  });

  it("redoes what it undid", () => {
    const h = [{ type: "undo" } as Act, { type: "redo" } as Act].reduce(
      reduce,
      run({ type: "type", field: "a", value: "one" }),
    );
    expect(h.present.text).toBe("one");
  });

  it("does nothing at either end", () => {
    expect(reduce(start, { type: "undo" })).toBe(start);
    expect(reduce(start, { type: "redo" })).toBe(start);
  });

  it("collapses consecutive edits to the same field into one step", () => {
    const h = run(
      { type: "type", field: "a", value: "o" },
      { type: "type", field: "a", value: "on" },
      { type: "type", field: "a", value: "one" },
    );
    expect(h.past).toHaveLength(1);
    expect(reduce(h, { type: "undo" }).present.text).toBe("");
  });

  it("starts a new step when the edit moves to another field", () => {
    const h = run(
      { type: "type", field: "a", value: "one" },
      { type: "type", field: "b", value: "two" },
    );
    expect(h.past).toHaveLength(2);
  });

  it("starts a new step when the selection moved in between", () => {
    const h = run(
      { type: "type", field: "a", value: "one" },
      { type: "look", at: "yaml" },
      { type: "type", field: "a", value: "onexx" },
    );
    expect(reduce(h, { type: "undo" }).present.text).toBe("one");
  });

  it("keeps view changes out of the past entirely", () => {
    const h = run({ type: "look", at: "yaml" }, { type: "look", at: "canvas" });
    expect(h.past).toHaveLength(0);
  });

  it("forgets the redo future once a new edit forks it", () => {
    const h = [
      { type: "undo" } as Act,
      { type: "type", field: "b", value: "other" } as Act,
    ].reduce(reduce, run({ type: "type", field: "a", value: "one" }));
    expect(h.future).toHaveLength(0);
  });

  it("ignores an action the reducer did not act on", () => {
    const h = run({ type: "type", field: "a", value: "one" });
    expect(reduce(h, { type: "noop" })).toBe(h);
  });

  it("drops the past when a different document is loaded", () => {
    const h = run({ type: "type", field: "a", value: "one" }, { type: "load", text: "fresh" });
    expect(h.past).toHaveLength(0);
    expect(reduce(h, { type: "undo" }).present.text).toBe("fresh");
  });

  it("caps the past so a long session cannot grow without bound", () => {
    const edits: Act[] = Array.from({ length: DEFAULT_LIMIT + 20 }, (_, i) => ({
      type: "type",
      field: `f${i}`,
      value: `v${i}`,
    }));
    expect(edits.reduce(reduce, start).past).toHaveLength(DEFAULT_LIMIT);
  });
});
