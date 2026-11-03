"use client";

import { useId, type ReactNode } from "react";

/**
 * A small card that opens under its trigger while the pointer is over it, or while
 * it holds keyboard focus. `children` receives the card's id, to set as
 * aria-describedby on the element the card describes, so a screen reader reads
 * the card with that control. Nothing in it should be interactive: it closes as
 * soon as the pointer leaves.
 */
export default function HoverCard({
  card,
  children,
}: {
  card: ReactNode;
  children: (cardId: string) => ReactNode;
}) {
  const id = useId();
  return (
    <span className="group relative inline-flex">
      {children(id)}
      <span
        id={id}
        role="tooltip"
        className="pointer-events-none invisible absolute left-0 top-full z-30 pt-2 opacity-0 transition duration-150 group-focus-within:visible group-focus-within:opacity-100 group-hover:visible group-hover:opacity-100"
      >
        <span className="block w-72 rounded-lg border border-black/10 bg-white p-3 text-left shadow-xl dark:border-white/10 dark:bg-zinc-900">
          {card}
        </span>
      </span>
    </span>
  );
}
