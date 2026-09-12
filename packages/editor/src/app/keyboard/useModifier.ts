"use client";

import { useEffect, useState } from "react";

/**
 * How to write the command modifier in a tooltip: `⌘` on a Mac, `Ctrl+` elsewhere.
 *
 * Resolved after mount rather than during render, because the answer is the browser's
 * and the server has no browser. Rendering the Mac symbol on the server would be a
 * guess; rendering the non-Mac one and then correcting it during render would be a
 * hydration mismatch on exactly the platform most of these users are on. Starting at
 * the neutral answer and updating in an effect is neither.
 */
export function useModifier(): string {
  const [modifier, setModifier] = useState("Ctrl+");

  useEffect(() => {
    // The user agent rather than the deprecated `navigator.platform`, and a substring
    // test rather than an exact match: the question is only which symbol to print in a
    // tooltip, and a wrong guess costs exactly that.
    if (/Mac|iPhone|iPad/.test(navigator.userAgent ?? "")) setModifier("⌘");
  }, []);

  return modifier;
}
