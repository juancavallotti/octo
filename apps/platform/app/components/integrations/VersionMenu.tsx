"use client";

import { useEffect, useRef, useState } from "react";
import { Check, ChevronDown, GitBranch, Tag } from "lucide-react";
import type { Snapshot } from "@/app/model/orchestrator";

/**
 * A GitHub-style version picker: a button showing the active version that opens a
 * popover menu of "Current" (the working copy) plus every tag, with a check on the
 * selected one and a green "deployed" dot on tags that are live. Closes on outside
 * click or Escape. Selecting a version drives the whole detail pane (Resources/Env
 * scope, the Deploy target, the deployments filter), so it lives in the header.
 */
export default function VersionMenu({
  snapshots,
  deployedTags,
  value,
  onChange,
}: {
  snapshots: Snapshot[];
  deployedTags: ReadonlySet<string>;
  /** The active tag, or null for Current (the working copy). */
  value: string | null;
  onChange: (tag: string | null) => void;
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

  const pick = (tag: string | null) => {
    onChange(tag);
    setOpen(false);
  };

  const label = value ?? "Current";

  return (
    <div ref={ref} className="relative shrink-0">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="menu"
        aria-expanded={open}
        title="Select version"
        className="inline-flex items-center gap-1.5 rounded-md border border-black/10 bg-transparent px-2.5 py-1 text-sm font-medium transition-colors hover:bg-black/[0.04] dark:border-white/15 dark:hover:bg-white/[0.06]"
      >
        <GitBranch size={14} className="text-zinc-400" />
        <span className="max-w-[10rem] truncate">{label}</span>
        <ChevronDown size={14} className="text-zinc-400" />
      </button>

      {open && (
        <div
          role="menu"
          className="absolute right-0 top-full z-30 mt-1 max-h-72 w-56 overflow-y-auto rounded-lg border border-black/10 bg-white py-1 shadow-lg dark:border-white/10 dark:bg-zinc-900"
        >
          <button
            type="button"
            role="menuitemradio"
            aria-checked={value === null}
            onClick={() => pick(null)}
            className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-sm hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
          >
            <Check
              size={14}
              className={value === null ? "text-sky-500" : "invisible"}
            />
            <span className="flex-1 truncate">Current</span>
            <span className="text-xs text-zinc-400">working copy</span>
          </button>

          {snapshots.length > 0 && (
            <div className="my-1 border-t border-black/5 dark:border-white/5" />
          )}

          {snapshots.map((s) => {
            const deployed = deployedTags.has(s.tag);
            const active = value === s.tag;
            return (
              <button
                key={s.id}
                type="button"
                role="menuitemradio"
                aria-checked={active}
                onClick={() => pick(s.tag)}
                className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-sm hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
              >
                <Check
                  size={14}
                  className={active ? "text-sky-500" : "invisible"}
                />
                <Tag size={12} className="shrink-0 text-zinc-400" />
                <span className="flex-1 truncate">{s.tag}</span>
                {deployed && (
                  <span
                    className="inline-flex items-center gap-1 text-xs text-emerald-600 dark:text-emerald-400"
                    title="Deployed"
                  >
                    <span className="h-1.5 w-1.5 rounded-full bg-emerald-500" />
                    deployed
                  </span>
                )}
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}
