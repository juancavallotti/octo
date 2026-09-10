"use client";

import { useEffect, useRef, useState } from "react";
import { MoreVertical, Tag } from "lucide-react";
import DuplicateMenuItem from "./DuplicateMenuItem";
import TagForm from "./TagForm";

/**
 * The editor header's overflow menu: the things you do to an integration once in a
 * while — duplicate it, tag a version — behind a single ⋮ instead of two buttons
 * holding the middle of the bar next to Deploy and Save.
 *
 * "Tag version…" swaps the menu's body for the tag form rather than opening a
 * second layer of popover, the way the connections launcher expands its add
 * submenu.
 */
export default function MoreMenu({
  getIntegrationId,
}: {
  /** Reads the authoritative integration id (updated on save). */
  getIntegrationId: () => string | null;
}) {
  const [open, setOpen] = useState(false);
  const [tagging, setTagging] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  const close = () => {
    setOpen(false);
    setTagging(false);
  };

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) close();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close();
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        aria-label="More actions"
        title="More actions"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => (open ? close() : setOpen(true))}
        className="rounded-md p-1.5 text-zinc-500 transition-colors hover:bg-black/[0.05] hover:text-zinc-800 dark:text-zinc-400 dark:hover:bg-white/[0.08] dark:hover:text-zinc-100"
      >
        <MoreVertical size={16} />
      </button>

      {open && (
        <div className="absolute right-0 top-full z-40 mt-2 w-56 overflow-hidden rounded-xl border border-black/10 bg-white shadow-lg dark:border-white/10 dark:bg-zinc-900">
          {tagging ? (
            <TagForm getIntegrationId={getIntegrationId} onDone={close} />
          ) : (
            <>
              <DuplicateMenuItem
                getIntegrationId={getIntegrationId}
                onDone={close}
              />
              <button
                type="button"
                onClick={() => setTagging(true)}
                className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm transition-colors hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
              >
                <Tag size={15} className="shrink-0 text-zinc-400" />
                Tag version…
              </button>
            </>
          )}
        </div>
      )}
    </div>
  );
}
