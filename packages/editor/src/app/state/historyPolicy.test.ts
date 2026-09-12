import { describe, expect, it } from "vitest";
import { EditorActionType } from "./actions";
import { coalesceKey, trackingOf } from "./historyPolicy";

describe("trackingOf", () => {
  it("classifies every action", () => {
    // The switch is exhaustive, so this cannot fail without the type-check failing
    // first — which is the point. It stands guard over `trackingOf` being reduced to
    // something with a default branch later on.
    for (const type of Object.values(EditorActionType)) {
      expect(["record", "skip", "reset"]).toContain(trackingOf(type));
    }
  });

  it("records document edits and skips the view", () => {
    expect(trackingOf(EditorActionType.ADD_BLOCK)).toBe("record");
    expect(trackingOf(EditorActionType.SET_ENV)).toBe("record");
    expect(trackingOf(EditorActionType.SELECT_BLOCK)).toBe("skip");
    expect(trackingOf(EditorActionType.SET_VIEW_MODE)).toBe("skip");
    expect(trackingOf(EditorActionType.LOAD_INTEGRATION)).toBe("reset");
  });
});

describe("coalesceKey", () => {
  const setting = (blockId: string, field: string) => ({
    type: EditorActionType.UPDATE_BLOCK_SETTING,
    data: { blockId, field, value: "x" },
  });

  it("joins keystrokes in one field", () => {
    expect(coalesceKey(setting("b1", "url"))).toBe(coalesceKey(setting("b1", "url")));
  });

  it("separates fields and blocks", () => {
    expect(coalesceKey(setting("b1", "url"))).not.toBe(coalesceKey(setting("b1", "method")));
    expect(coalesceKey(setting("b1", "url"))).not.toBe(coalesceKey(setting("b2", "url")));
  });

  it("leaves structural edits uncoalesced, so each is its own step", () => {
    expect(coalesceKey({ type: EditorActionType.ADD_BLOCK, data: { blockType: "log" } })).toBeNull();
    expect(coalesceKey({ type: EditorActionType.REMOVE_BLOCK, data: {} })).toBeNull();
  });
});
