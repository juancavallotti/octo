"use client";

import { useEffect, useRef, useState } from "react";
import { Cloud, CloudOff, Loader2 } from "lucide-react";
import { paasBridge } from "../desktop";
import IdentityDot from "../platform/components/IdentityDot";
import { usePlatformState } from "../platform/usePlatformState";
import type { PlatformState } from "../platform/types";

type Item = { label: string; focus?: string; push?: true } | "separator";

function menuFor(state: PlatformState): Item[] {
  if (state.link) {
    return [
      { label: "Push to platform", push: true },
      { label: "Versions…", focus: "versions" },
      { label: "Deploy…", focus: "deployments" },
      { label: "Logs…", focus: "logs" },
      "separator",
      { label: "Open Platform", focus: "folder" },
      { label: "Installations…", focus: "settings" },
    ];
  }
  return [
    { label: "Put this folder on the platform…", focus: "folder" },
    { label: "Pull an integration…", focus: "integrations" },
    "separator",
    { label: "Installations…", focus: "settings" },
  ];
}

/**
 * Which integration the open folder is on the platform, beside the folder chip, and
 * the way to everything the platform does with it. It also carries the last platform
 * operation — a push, a deploy — so one started from the menu is seen to happen.
 * Rendered only under a shell that drives the platform.
 */
export default function PlatformChip() {
  const state = usePlatformState();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const paas = paasBridge();
  if (!paas || !state) return null;

  const { link, activity } = state;
  const installation = link ? state.profiles.find((p) => p.name === link.profile) : undefined;
  const failed = activity?.state === "failed";
  const title = activity
    ? `${activity.label}${activity.state === "running" ? "…" : failed ? ` failed: ${activity.message ?? ""}` : " — done"}`
    : link
      ? `${link.integrationName} on ${link.profile} (${link.url})`
      : "This folder is not on a platform";

  const choose = (item: Exclude<Item, "separator">) => {
    setOpen(false);
    if (item.push) void paas.push().catch(() => {});
    else void paas.open(item.focus);
  };

  return (
    <div ref={ref} className="relative min-w-0">
      <button
        type="button"
        aria-label="Platform"
        aria-expanded={open}
        title={title}
        onClick={() => setOpen((v) => !v)}
        className="flex min-w-0 items-center gap-1.5 rounded-md border border-transparent px-2 py-1 text-sm text-zinc-600 transition-colors hover:border-black/10 hover:text-zinc-900 dark:text-zinc-300 dark:hover:border-white/15 dark:hover:text-zinc-100"
      >
        {activity?.state === "running" ? (
          <Loader2 size={14} className="shrink-0 animate-spin text-sky-500" />
        ) : link ? (
          <Cloud size={14} className={`shrink-0 ${failed ? "text-rose-500" : "text-sky-500"}`} />
        ) : (
          <CloudOff size={14} className="shrink-0 text-zinc-400" />
        )}
        {link && (
          <span className="min-w-0 max-w-[12rem] truncate max-[1100px]:sr-only">
            {link.profile}
            {link.pulledTag ? ` · ${link.pulledTag}` : ""}
          </span>
        )}
        {installation && installation.identity.state !== "signed-in" && <IdentityDot identity={installation.identity} />}
      </button>
      {open && (
        <div className="absolute left-0 top-full z-50 mt-2 w-64 overflow-hidden rounded-xl border border-black/10 bg-white py-1 shadow-lg dark:border-white/10 dark:bg-zinc-900">
          <div className="px-3 py-2 text-xs text-zinc-500">{title}</div>
          {menuFor(state).map((item, i) =>
            item === "separator" ? (
              <div key={i} className="my-1 border-t border-black/5 dark:border-white/5" />
            ) : (
              <button
                key={item.label}
                type="button"
                disabled={item.push && activity?.state === "running"}
                onClick={() => choose(item)}
                className="flex w-full items-center px-3 py-1.5 text-left text-sm transition-colors hover:bg-black/[0.04] disabled:opacity-40 dark:hover:bg-white/[0.06]"
              >
                {item.label}
              </button>
            ),
          )}
        </div>
      )}
    </div>
  );
}
