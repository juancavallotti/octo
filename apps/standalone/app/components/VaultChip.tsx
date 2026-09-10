"use client";

import { useEffect, useRef, useState } from "react";
import { Check, Copy, FolderOpen, FolderSearch } from "lucide-react";
import { desktopBridge, type VaultRef } from "../desktop";

/**
 * Which folder the desktop shell is serving, and how to change it.
 *
 * Desktop only. Choosing a folder is a shell capability — only Electron can open
 * a native picker or restart itself on another directory — and in a browser the
 * chip was a dead label for something the user could not act on. `task dev` and
 * the Docker image are configured by whoever started them, not from in here.
 *
 * It lives on the right of the header, with Save and the rest of the
 * project-level controls, rather than inline with the flow's own title.
 */
export default function VaultChip() {
  const bridge = desktopBridge();
  const [vault, setVault] = useState<VaultRef | null>(null);
  const [recents, setRecents] = useState<VaultRef[]>([]);
  const [copied, setCopied] = useState(false);
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!bridge) return;
    let cancelled = false;
    bridge
      .vault()
      .then((v) => {
        if (!cancelled) setVault(v);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [bridge]);

  // Refreshed on open rather than once, so a folder opened via the app menu
  // shows up without a reload.
  useEffect(() => {
    if (!open || !bridge) return;
    bridge.recents().then(setRecents).catch(() => {});
  }, [open, bridge]);

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

  // Nothing to show until the shell answers — which also keeps the first client
  // render identical to the server's, so hydration has nothing to disagree about.
  if (!bridge || !vault) return null;

  const copyUrl = async () => {
    await bridge.copyMcpUrl();
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  const item =
    "flex w-full items-center gap-2 px-3 py-2 text-left text-sm transition-colors hover:bg-black/[0.04] dark:hover:bg-white/[0.06]";

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        title={vault.path || undefined}
        aria-label="Project folder"
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-1.5 rounded-md border border-transparent px-2 py-1 text-sm text-zinc-600 transition-colors hover:border-black/10 hover:text-zinc-900 dark:text-zinc-300 dark:hover:border-white/15 dark:hover:text-zinc-100"
      >
        <FolderOpen size={14} className="shrink-0 text-zinc-400" />
        <span className="max-w-[10rem] truncate">{vault.name}</span>
      </button>

      {open && (
        /* Anchored to the right edge: the chip now sits near the end of the bar,
           so a left-anchored menu would hang off it. */
        <div className="absolute right-0 top-full z-50 mt-2 w-72 overflow-hidden rounded-xl border border-black/10 bg-white shadow-lg dark:border-white/10 dark:bg-zinc-900">
          <button type="button" onClick={() => void bridge.pickVault()} className={`${item} border-b border-black/5 dark:border-white/5`}>
            <FolderSearch size={16} className="shrink-0 text-zinc-400" />
            <span className="flex-1">Open folder…</span>
          </button>

          <ul className="max-h-64 overflow-y-auto py-1">
            <li>
              <span className={`${item} cursor-default hover:bg-transparent dark:hover:bg-transparent`}>
                <span className="flex-1 truncate">{vault.name}</span>
                <Check size={15} className="shrink-0 text-sky-500" />
              </span>
            </li>
            {recents.map((v) => (
              <li key={v.path}>
                <button
                  type="button"
                  title={v.path}
                  onClick={() => void bridge.switchVault(v.path)}
                  className={item}
                >
                  <span className="flex-1 truncate">{v.name}</span>
                </button>
              </li>
            ))}
          </ul>

          <button type="button" onClick={() => void copyUrl()} className={`${item} border-t border-black/5 dark:border-white/5`}>
            <Copy size={16} className="shrink-0 text-zinc-400" />
            {/* The MCP endpoint is the whole reason an agent can drive this
                editor, and it is otherwise invisible from inside it. */}
            <span className="flex-1">{copied ? "Copied" : "Copy MCP endpoint URL"}</span>
          </button>
        </div>
      )}
    </div>
  );
}
