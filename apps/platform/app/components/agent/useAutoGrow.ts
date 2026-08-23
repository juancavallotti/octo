"use client";

import { useLayoutEffect, useRef } from "react";

/**
 * A textarea that is as tall as what has been typed into it, up to a limit.
 *
 * The composer was a one-line box with a `max-h-32` that never applied, because
 * the height never changed for it to clamp. Anything longer than a sentence
 * scrolled inside a single visible line, which is a poor way to write the kind
 * of question this panel is for.
 *
 * The measurement is the whole trick and it has one rule: the height has to be
 * released before it is read. `scrollHeight` is the content's height *or the
 * element's*, whichever is larger, so measuring an element still holding last
 * keystroke's height can only ever grow — deleting a line would leave the box
 * where it was, permanently.
 */

/**
 * Attaches to a textarea and keeps it sized to `value`, up to `maxRows` lines.
 * Past that it stops growing and scrolls, which is what the rows are a limit for.
 *
 * `value` is passed rather than read off the element because the resize belongs
 * to the render that changed it: a controlled textarea whose value came from
 * elsewhere — a cleared draft, a restored one — has to resize too, and there is
 * no input event for either.
 */
export function useAutoGrow(value: string, maxRows: number) {
  const ref = useRef<HTMLTextAreaElement | null>(null);

  // Layout, not effect: this runs between React writing the value and the browser
  // painting it. In a plain effect the old height is on screen for a frame, which
  // reads as the box flickering on every keystroke.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;

    // Release, then measure. See the note above — this order is the point.
    el.style.height = "auto";

    // The cap is computed from the element's own line-height rather than a class,
    // so the clamp and what the user sees cannot drift apart. A jsdom element (or
    // one styled in `em`) reports no usable line-height; falling back to the
    // unclamped height keeps the hook honest instead of collapsing the box to zero.
    const lineHeight = parseFloat(getComputedStyle(el).lineHeight);
    const content = el.scrollHeight;
    if (!Number.isFinite(lineHeight) || lineHeight <= 0) {
      el.style.height = `${content}px`;
      return;
    }

    // The padding is not part of the text, so it is added to the line budget
    // rather than eating into it: `maxRows` means rows of writing.
    const { paddingTop, paddingBottom } = getComputedStyle(el);
    const chrome = parseFloat(paddingTop) + parseFloat(paddingBottom);
    const cap = lineHeight * maxRows + (Number.isFinite(chrome) ? chrome : 0);

    el.style.height = `${Math.min(content, cap)}px`;
    // Only the clamped state scrolls. Left on, a box with room to spare still
    // shows a scrollbar gutter in the browsers that reserve one.
    el.style.overflowY = content > cap ? "auto" : "hidden";
  }, [value, maxRows]);

  return ref;
}
