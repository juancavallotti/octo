"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { Check, ChevronDown, Layers, Settings2, TriangleAlert } from "lucide-react";
import type { EnvironmentOption } from "./environmentSwitch";

/**
 * The switcher's chip and menu: the environment's display name on the chip, and
 * in the menu each one's display name over its name and namespace, its
 * description, and a check on the current one. Closes on a pick, a click
 * outside, or Escape.
 */
export default function EnvironmentMenu({
  options,
  current,
  busy,
  error,
  manageHref,
  onChoose,
}: {
  options: EnvironmentOption[];
  current: string;
  busy: boolean;
  /** Why the last switch was refused, said beside the chip until the next one. */
  error?: string | null;
  /** Where an administrator manages environments; absent for anybody else. */
  manageHref?: string;
  onChoose(name: string): void;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const selected = options.find((o) => o.name === current);
  // Tinted for any environment but the default, whether or not the list could be
  // read: the tint is the warning that this is not the default.
  const elsewhere = selected ? !selected.isDefault : current !== "";

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setOpen(false);
        buttonRef.current?.focus();
      }
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  return (
    <div ref={ref} className="relative flex items-center gap-2">
      {error && (
        <span
          role="alert"
          title={error}
          className="inline-flex items-center gap-1 rounded-md border border-amber-500/25 bg-amber-500/5 px-2 py-0.5 text-xs text-zinc-700 dark:text-zinc-300"
        >
          <TriangleAlert size={12} className="shrink-0 text-amber-600 dark:text-amber-500" aria-hidden />
          Couldn&rsquo;t switch
        </span>
      )}
      <button
        ref={buttonRef}
        type="button"
        aria-label="Environment"
        aria-haspopup="menu"
        aria-expanded={open}
        disabled={busy}
        onClick={() => setOpen((v) => !v)}
        className={`flex items-center gap-1.5 rounded-md border px-2 py-1 text-sm transition-colors disabled:opacity-60 ${
          elsewhere
            ? "border-amber-500/40 bg-amber-500/10 text-amber-800 dark:text-amber-200"
            : "border-black/10 text-zinc-700 hover:border-black/20 dark:border-white/15 dark:text-zinc-200"
        }`}
      >
        <Layers size={14} className="shrink-0 opacity-70" aria-hidden />
        <span className="max-w-[10rem] truncate @max-lg:max-w-[6rem]">{selected?.label ?? (current || "Default")}</span>
        <ChevronDown size={14} className="shrink-0 opacity-60" aria-hidden />
      </button>
      {open && (
        <div
          role="menu"
          aria-label="Environments"
          className="absolute right-0 top-full z-50 mt-1 w-72 overflow-hidden rounded-lg border border-black/10 bg-white shadow-lg dark:border-white/10 dark:bg-zinc-900"
        >
          <p className="border-b border-black/5 px-3 py-2 text-[11px] font-medium uppercase tracking-wide text-zinc-400 dark:border-white/5">
            Environment
          </p>
          <ul className="max-h-80 overflow-y-auto py-1">
            {options.map((o) => {
              const isCurrent = o.name === current;
              return (
                <li key={o.slug}>
                  <button
                    type="button"
                    role="menuitemradio"
                    aria-checked={isCurrent}
                    onClick={() => {
                      setOpen(false);
                      if (!isCurrent) onChoose(o.name);
                    }}
                    className={`flex w-full items-start gap-2 px-3 py-2 text-left transition-colors ${
                      isCurrent ? "bg-sky-500/10" : "hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
                    }`}
                  >
                    <span className="mt-0.5 w-4 shrink-0 text-sky-600 dark:text-sky-400">
                      {isCurrent && <Check size={14} aria-hidden />}
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="flex items-center gap-1.5">
                        <span className="truncate text-sm font-medium text-zinc-800 dark:text-zinc-100">{o.label}</span>
                        {o.isDefault && (
                          <span className="rounded bg-zinc-500/10 px-1 text-[10px] font-medium uppercase text-zinc-500">
                            Default
                          </span>
                        )}
                      </span>
                      <span className="block truncate font-mono text-[11px] text-zinc-500">
                        {o.slug} · {o.namespace}
                      </span>
                      {o.description && (
                        <span className="mt-0.5 block line-clamp-2 text-xs text-zinc-500">{o.description}</span>
                      )}
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
          {manageHref && (
            <Link
              href={manageHref}
              onClick={() => setOpen(false)}
              className="flex items-center gap-2 border-t border-black/5 px-3 py-2 text-sm text-zinc-600 hover:bg-black/[0.04] dark:border-white/5 dark:text-zinc-300 dark:hover:bg-white/[0.06]"
            >
              <Settings2 size={14} className="text-zinc-400" aria-hidden />
              Manage environments…
            </Link>
          )}
        </div>
      )}
    </div>
  );
}
