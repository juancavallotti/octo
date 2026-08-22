"use client";

import { useCallback, useEffect, useRef, useState } from "react";

/**
 * A scroller that follows new content, and stops the moment you scroll away from
 * it.
 *
 * The panel used to jump to the bottom on every token, which is right while you
 * are reading the newest text and wrong the instant you are not: scrolling up to
 * re-read what a tool returned meant being dragged back down on the next token,
 * a few times a second. There was no way to read anything but the end of an
 * answer while one was arriving.
 *
 * So following is conditional on already being at the end. Leaving the bottom is
 * how you turn it off and returning is how you turn it back on, which needs no
 * control and no explanation.
 */

/** How close to the end still counts as being at it, in pixels. */
const BOTTOM_SLACK = 48;

export interface StickToBottom {
  /**
   * Attach to the scrolling element. A callback ref rather than an object one, so
   * the listener attaches when the node appears rather than on a mount that ran
   * before it existed.
   */
  ref: (node: HTMLDivElement | null) => void;
  /** Whether the view is following new content. */
  following: boolean;
  /** Go back to the end, and start following again. */
  toBottom: () => void;
}

export function useStickToBottom(dep: unknown): StickToBottom {
  const [el, setEl] = useState<HTMLDivElement | null>(null);
  const [following, setFollowing] = useState(true);

  const toBottom = useCallback(() => {
    if (!el) return;
    el.scrollTo({ top: el.scrollHeight });
    setFollowing(true);
  }, [el]);

  // Read the position on every scroll rather than trying to tell a user's scroll
  // from ours: the two are indistinguishable from an event, and the answer we
  // actually want — "is the end in view" — is a property of where it ended up.
  useEffect(() => {
    if (!el) return;
    const onScroll = () => {
      const distance = el.scrollHeight - el.scrollTop - el.clientHeight;
      setFollowing(distance <= BOTTOM_SLACK);
    };
    el.addEventListener("scroll", onScroll, { passive: true });
    return () => el.removeEventListener("scroll", onScroll);
  }, [el]);

  // Read through a ref rather than depending on it: scrolling is a response to
  // new content, and taking `following` as a dependency would also scroll the
  // moment it flips true — snapping the view down by the slack below when someone
  // scrolled back to *near* the end, which they did not ask for.
  // Mirrored in an effect rather than during render, as the callback ref in
  // useAgentChat is: a render can be discarded, and this is what the next
  // commit's scroll will read.
  const followingRef = useRef(following);
  useEffect(() => {
    followingRef.current = following;
  }, [following]);

  useEffect(() => {
    if (!followingRef.current || !el) return;
    el.scrollTo({ top: el.scrollHeight });
  }, [dep, el]);

  return { ref: setEl, following, toBottom };
}
