"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { Check, FilePlus, FolderOpen } from "lucide-react";
import { useFileSystem, type StoredDocument } from "@octo/editor";

/**
 * Open/new menu for the standalone editor. Lists the `*.yaml` flows in the local
 * store (via the filesystem capability) and links to `/?file=<id>` to open one,
 * or `/` for a fresh document. Renders nothing without a filesystem capability.
 */
export default function StandaloneFileMenu({ current }: { current?: string }) {
  const fs = useFileSystem();
  const [open, setOpen] = useState(false);
  const [files, setFiles] = useState<StoredDocument[]>([]);
  const ref = useRef<HTMLDivElement>(null);

  // Refresh the list whenever the menu opens, so a just-saved file shows up.
  useEffect(() => {
    if (!open || !fs?.list) return;
    fs.list()
      .then(setFiles)
      .catch(() => {});
  }, [open, fs]);

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

  if (!fs) return null;

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-1.5 rounded-md border border-transparent px-2 py-1 text-sm text-zinc-600 transition-colors hover:border-black/10 hover:text-zinc-900 dark:text-zinc-300 dark:hover:border-white/15 dark:hover:text-zinc-100"
      >
        <FolderOpen size={15} />
        <span className="max-w-[12rem] truncate">{current ?? "Open"}</span>
      </button>

      {open && (
        <div className="absolute left-0 top-full z-50 mt-2 w-64 overflow-hidden rounded-xl border border-black/10 bg-white shadow-lg dark:border-white/10 dark:bg-zinc-900">
          <Link
            href="/"
            onClick={() => setOpen(false)}
            className="flex items-center gap-2 border-b border-black/5 px-3 py-2 text-sm transition-colors hover:bg-black/[0.04] dark:border-white/5 dark:hover:bg-white/[0.06]"
          >
            <FilePlus size={16} className="shrink-0 text-zinc-400" />
            <span className="flex-1">New flow</span>
          </Link>
          <ul className="max-h-72 overflow-y-auto py-1">
            {files.length === 0 && (
              <li className="px-3 py-2 text-xs text-zinc-400">
                No saved flows yet
              </li>
            )}
            {files.map((f) => (
              <li key={f.id}>
                <Link
                  href={`/?file=${encodeURIComponent(f.id)}`}
                  onClick={() => setOpen(false)}
                  className="flex items-center gap-2 px-3 py-2 text-left text-sm transition-colors hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
                >
                  <span className="flex-1 truncate">{f.name}</span>
                  {current === f.id && (
                    <Check size={15} className="text-sky-500" />
                  )}
                </Link>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
