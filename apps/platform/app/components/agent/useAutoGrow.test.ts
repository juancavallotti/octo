/**
 * The sizing rule, which has one failure mode worth a test of its own: a box that
 * grows and never shrinks. jsdom lays nothing out, so the content height is
 * stubbed — what is under test is the arithmetic and the order of operations, not
 * the browser's.
 */

import { describe, expect, it } from "vitest";
import { act, renderHook } from "@testing-library/react";

import { useAutoGrow } from "./useAutoGrow";

const LINE = 20;
const PADDING = 6;

/**
 * A textarea whose content height follows whatever was last written to it.
 *
 * jsdom reports scrollHeight as 0 and computes no line-height, so both are stood
 * up here. scrollHeight is a getter rather than a value because the hook releases
 * the height before reading it, and a test that could not tell the two apart
 * would pass whether or not the hook did.
 */
function textareaOf() {
  const el = document.createElement("textarea");
  let content = LINE + PADDING;

  Object.defineProperty(el, "scrollHeight", {
    configurable: true,
    // Held height wins, exactly as a browser reports it. This is what makes the
    // release-then-measure order observable.
    get: () => {
      const held = parseFloat(el.style.height);
      return Number.isFinite(held) ? Math.max(content, held) : content;
    },
  });

  el.style.lineHeight = `${LINE}px`;
  el.style.paddingTop = `${PADDING / 2}px`;
  el.style.paddingBottom = `${PADDING / 2}px`;

  return {
    el,
    /** Set what the content would measure at, in lines. */
    lines: (n: number) => {
      content = LINE * n + PADDING;
    },
  };
}

function mount(maxRows = 4) {
  const { el, lines } = textareaOf();
  const hook = renderHook(({ value }) => useAutoGrow(value, maxRows), {
    initialProps: { value: "" },
  });
  act(() => {
    hook.result.current.current = el;
    hook.rerender({ value: "" });
  });
  return { el, lines, hook };
}

/** The height the hook settled on, in pixels. */
const heightOf = (el: HTMLTextAreaElement) => parseFloat(el.style.height);

describe("useAutoGrow", () => {
  it("grows to fit the draft", () => {
    const { el, lines, hook } = mount();

    act(() => {
      lines(3);
      hook.rerender({ value: "one\ntwo\nthree" });
    });

    expect(heightOf(el)).toBe(LINE * 3 + PADDING);
  });

  it("stops at maxRows and scrolls past it", () => {
    const { el, lines, hook } = mount(4);

    act(() => {
      lines(9);
      hook.rerender({ value: "a very long draft" });
    });

    expect(heightOf(el)).toBe(LINE * 4 + PADDING);
    expect(el.style.overflowY).toBe("auto");
  });

  it("shrinks again when the draft is deleted", () => {
    const { el, lines, hook } = mount();

    act(() => {
      lines(4);
      hook.rerender({ value: "four lines of it" });
    });
    expect(heightOf(el)).toBe(LINE * 4 + PADDING);

    // The regression this hook exists to prevent: without releasing the height
    // before measuring, scrollHeight still reports the four lines it is holding.
    act(() => {
      lines(1);
      hook.rerender({ value: "" });
    });

    expect(heightOf(el)).toBe(LINE + PADDING);
    expect(el.style.overflowY).toBe("hidden");
  });

  it("falls back to the content height when no line-height is computable", () => {
    const { el, lines, hook } = mount();
    el.style.lineHeight = "";

    act(() => {
      lines(12);
      hook.rerender({ value: "unmeasurable" });
    });

    // Unclamped rather than collapsed: an unknown line-height is a reason to
    // stop clamping, never a reason to hide the draft.
    expect(heightOf(el)).toBe(LINE * 12 + PADDING);
  });

  it("does nothing before the textarea is attached", () => {
    const hook = renderHook(({ value }) => useAutoGrow(value, 4), {
      initialProps: { value: "" },
    });

    expect(() => act(() => hook.rerender({ value: "typed" }))).not.toThrow();
    expect(hook.result.current.current).toBeNull();
  });
});
