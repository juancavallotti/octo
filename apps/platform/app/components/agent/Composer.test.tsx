/**
 * The composer's two contracts: the keyboard conventions, which were never
 * covered, and the box growing with the draft, which is why this file exists.
 */

import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

import Composer from "./Composer";

const LINE = 20;
const PADDING = 6;

/**
 * jsdom lays nothing out, so the textarea is given the two measurements the hook
 * reads. scrollHeight follows the value's line count, and a held height still
 * wins — the browser's own rule, and the one the shrink case turns on.
 */
function measurable(content = () => LINE + PADDING) {
  const proto = window.HTMLTextAreaElement.prototype;
  const original = Object.getOwnPropertyDescriptor(proto, "scrollHeight");
  Object.defineProperty(proto, "scrollHeight", {
    configurable: true,
    get(this: HTMLTextAreaElement) {
      const held = parseFloat(this.style.height);
      return Number.isFinite(held) ? Math.max(content(), held) : content();
    },
  });
  return () => {
    if (original) Object.defineProperty(proto, "scrollHeight", original);
  };
}

/**
 * Mounts empty and hands back a `type` that re-renders with a new draft.
 *
 * Empty first because the element does not exist to be given its measurements
 * until after the first render, and the hook has already run by then — so the
 * mount is never the render under test.
 */
function draw() {
  const onSubmit = vi.fn();
  const onDraft = vi.fn();
  const render1 = (draft: string) => (
    <Composer
      draft={draft}
      onDraft={onDraft}
      onSubmit={onSubmit}
      busy={false}
      onStop={vi.fn()}
    />
  );

  const view = render(render1(""));
  const box = screen.getByLabelText("Message") as HTMLTextAreaElement;
  box.style.lineHeight = `${LINE}px`;
  box.style.paddingTop = `${PADDING / 2}px`;
  box.style.paddingBottom = `${PADDING / 2}px`;

  return {
    view,
    box,
    onSubmit,
    onDraft,
    type: (draft: string) => view.rerender(render1(draft)),
    /** The height the hook settled on, in pixels. */
    height: () => parseFloat(box.style.height),
  };
}

describe("Composer", () => {
  it("carries no max-height class: the cap is measured, not styled", () => {
    const { box } = draw();

    // Two answers to one question is how the old `max-h-32` came to be dead code
    // — it clamped a height that never changed.
    expect(box.className).not.toMatch(/max-h-/);
  });

  it("grows with the draft and stops at four lines", () => {
    let lines = 1;
    const restore = measurable(() => LINE * lines + PADDING);
    try {
      const { box, type, height } = draw();

      lines = 3;
      type("a\nb\nc");
      expect(height()).toBe(LINE * 3 + PADDING);

      lines = 12;
      type("a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl");
      expect(height()).toBe(LINE * 4 + PADDING);
      expect(box.style.overflowY).toBe("auto");
    } finally {
      restore();
    }
  });

  it("shrinks back to one line once the draft is sent", () => {
    let lines = 1;
    const restore = measurable(() => LINE * lines + PADDING);
    try {
      const { box, type, height } = draw();

      lines = 5;
      type("five\nlines\nof\na\ndraft");
      expect(height()).toBe(LINE * 4 + PADDING);

      // Submitting clears the draft in the drawer above; the box has to follow it
      // back down rather than keep the space it grew into. Without releasing the
      // height before measuring, scrollHeight still reports the four lines it is
      // holding and the box never comes back.
      lines = 1;
      type("");

      expect(height()).toBe(LINE + PADDING);
      expect(box.style.overflowY).toBe("hidden");
    } finally {
      restore();
    }
  });

  it("sends on Enter", () => {
    const { box, onSubmit, type } = draw();
    type("ready");

    fireEvent.keyDown(box, { key: "Enter" });

    expect(onSubmit).toHaveBeenCalledOnce();
  });

  it("breaks the line on shift+Enter instead of sending", () => {
    const { box, onSubmit, type } = draw();
    type("ready");

    fireEvent.keyDown(box, { key: "Enter", shiftKey: true });

    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("does not send an Enter that is accepting an IME candidate", () => {
    const { box, onSubmit, type } = draw();
    type("日本");

    fireEvent.keyDown(box, { key: "Enter", isComposing: true });

    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("refuses a draft that is only whitespace, both ways in", () => {
    const { box, onSubmit, type } = draw();
    type("   ");

    fireEvent.keyDown(box, { key: "Enter" });
    expect(onSubmit).not.toHaveBeenCalled();

    expect((screen.getByLabelText("Send") as HTMLButtonElement).disabled).toBe(
      true,
    );
  });
});
