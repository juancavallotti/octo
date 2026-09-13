"use client";

import { useEffect, useState } from "react";

/**
 * How to write the command modifier in a tooltip: `⌘` on a Mac, `Ctrl+` elsewhere.
 *
 * Resolved after mount rather than during render: the answer is the browser's, and
 * correcting it during render would be a hydration mismatch.
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
