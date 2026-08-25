"use client";

import { useEffect, useRef, useState } from "react";
import { Download, FileCode, Package } from "lucide-react";

/**
 * The header's export control: a download button that opens a two-item popover —
 * the definition on its own, or the whole integration as a bundle (its definition
 * plus every resource it owns).
 *
 * A menu rather than two buttons because the two are the same action at two
 * scopes, and the header is already crowded. It closes on outside click or
 * Escape, like the version picker beside it.
 */
export default function DownloadMenu({
  versionLabel,
  busy,
  onDownloadDefinition,
  onDownloadBundle,
}: {
  /** The active version, shown so it is clear *what* is being exported. */
  versionLabel: string;
  busy: boolean;
  onDownloadDefinition: () => void;
  onDownloadBundle: () => void;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const pick = (run: () => void) => {
    setOpen(false);
    run();
  };

  return (
    <div ref={ref} className="relative shrink-0">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        disabled={busy}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label="Download"
        title="Download"
        className="rounded-md p-1.5 text-zinc-400 transition-colors hover:bg-black/[0.06] hover:text-zinc-700 disabled:opacity-50 dark:hover:bg-white/10 dark:hover:text-zinc-200"
      >
        <Download size={16} />
      </button>

      {open && (
        <div
          role="menu"
          className="absolute right-0 top-full z-30 mt-1 w-64 rounded-lg border border-black/10 bg-white py-1 shadow-lg dark:border-white/10 dark:bg-zinc-900"
        >
          <p className="px-2.5 py-1 text-[10px] font-semibold uppercase tracking-wide text-zinc-400">
            Download {versionLabel}
          </p>
          <button
            type="button"
            role="menuitem"
            onClick={() => pick(onDownloadDefinition)}
            className="flex w-full items-start gap-2 px-2.5 py-1.5 text-left text-sm hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
          >
            <FileCode size={14} className="mt-0.5 shrink-0 text-zinc-400" />
            <span>
              Definition
              <span className="block text-xs text-zinc-400">
                The integration YAML on its own
              </span>
            </span>
          </button>
          <button
            type="button"
            role="menuitem"
            onClick={() => pick(onDownloadBundle)}
            className="flex w-full items-start gap-2 px-2.5 py-1.5 text-left text-sm hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
          >
            <Package size={14} className="mt-0.5 shrink-0 text-zinc-400" />
            <span>
              Bundle
              <span className="block text-xs text-zinc-400">
                A zip of the definition and every resource
              </span>
            </span>
          </button>
        </div>
      )}
    </div>
  );
}
