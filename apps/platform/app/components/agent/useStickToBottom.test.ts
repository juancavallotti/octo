/**
 * The scroll behaviour, which is the one thing in this panel that people
 * complained about by name: reading anything but the end of an answer while one
 * was arriving was impossible, because every token dragged the view back down.
 */

import { describe, expect, it } from "vitest";
import { act, renderHook } from "@testing-library/react";

import { useStickToBottom } from "./useStickToBottom";

/** A scroller of a known size, so "at the bottom" is a fact rather than a guess. */
function scrollerOf(scrollTop: number): HTMLDivElement {
  const el = document.createElement("div");
  Object.defineProperty(el, "scrollHeight", { value: 1000, configurable: true });
  Object.defineProperty(el, "clientHeight", { value: 200, configurable: true });
  el.scrollTop = scrollTop;
  el.scrollTo = ((options: ScrollToOptions) => {
    el.scrollTop = options.top ?? 0;
  }) as HTMLElement["scrollTo"];
  return el;
}

/** Mount the hook against a scroller, as React would attach it. */
function mount(scrollTop: number) {
  const el = scrollerOf(scrollTop);
  const hook = renderHook(({ dep }) => useStickToBottom(dep), {
    initialProps: { dep: 0 },
  });
  act(() => hook.result.current.ref(el));
  return { el, hook };
}

describe("useStickToBottom", () => {
  it("follows new content while the end is in view", () => {
    const { el, hook } = mount(800);

    act(() => {
      Object.defineProperty(el, "scrollHeight", { value: 1400, configurable: true });
      hook.rerender({ dep: 1 });
    });

    expect(hook.result.current.following).toBe(true);
    expect(el.scrollTop).toBe(1400);
  });

  // The complaint, in one assertion: scrolled up to re-read something, and the
  // next token pulled the view back down.
  it("stops following once you scroll away from the end", () => {
    const { el, hook } = mount(800);

    act(() => {
      el.scrollTop = 100;
      el.dispatchEvent(new Event("scroll"));
    });
    expect(hook.result.current.following).toBe(false);

    act(() => {
      Object.defineProperty(el, "scrollHeight", { value: 1400, configurable: true });
      hook.rerender({ dep: 1 });
    });
    expect(el.scrollTop).toBe(100);
  });

  it("follows again once the end is back in view", () => {
    const { el, hook } = mount(800);

    act(() => {
      el.scrollTop = 100;
      el.dispatchEvent(new Event("scroll"));
    });
    act(() => {
      el.scrollTop = 800;
      el.dispatchEvent(new Event("scroll"));
    });

    expect(hook.result.current.following).toBe(true);
  });

  it("goes back to the end on request", () => {
    const { el, hook } = mount(800);

    act(() => {
      el.scrollTop = 100;
      el.dispatchEvent(new Event("scroll"));
    });
    expect(hook.result.current.following).toBe(false);

    act(() => hook.result.current.toBottom());

    expect(el.scrollTop).toBe(1000);
    expect(hook.result.current.following).toBe(true);
  });
});
