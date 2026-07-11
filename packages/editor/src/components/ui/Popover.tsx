"use client";

import { useEffect, useRef, type ReactNode } from "react";

/**
 * A panel anchored to a trigger, dismissed by clicking outside it or pressing Escape.
 *
 * The editor has grown four of these by copy-paste (the connections, environment and
 * resources launchers, and the folder picker), each re-implementing the same listeners.
 * This is that recipe, once. It is *controlled* — the parent owns `open`, because the
 * parent is usually the one that needs to close it (after picking something) or open it
 * from elsewhere.
 *
 * Only the dismissal behavior lives here. The trigger and the panel's contents are the
 * caller's, so this imposes no styling on either.
 */
export default function Popover({
  open,
  onClose,
  trigger,
  children,
  /** Extra classes for the panel — width, and which edge it aligns to. */
  panelClassName = "left-0 w-64",
}: {
  open: boolean;
  onClose(): void;
  trigger: ReactNode;
  children: ReactNode;
  panelClassName?: string;
}) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) onClose();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open, onClose]);

  return (
    <div ref={ref} className="relative">
      {trigger}
      {open && (
        <div
          className={`absolute top-full z-30 mt-2 overflow-hidden rounded-xl border border-black/10 bg-white shadow-lg dark:border-white/10 dark:bg-zinc-900 ${panelClassName}`}
        >
          {children}
        </div>
      )}
    </div>
  );
}
