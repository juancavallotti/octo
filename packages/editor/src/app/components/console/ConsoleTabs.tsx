"use client";

import { KeyRound, ScrollText, TriangleAlert, Braces } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import type { ConsoleTab } from "../../run/console";

/**
 * The console's tab strip. Declarative, so a tab is one entry here rather than three
 * edits scattered through the panel. Each carries an icon: with four tabs, a row of
 * bare words is hard to scan, and the icon is what the eye actually lands on.
 */

interface TabSpec {
  key: ConsoleTab;
  label: string;
  icon: LucideIcon;
}

// Logs stays first: it is the tab a user looks at most, and the one Run opens.
const TABS: TabSpec[] = [
  { key: "logs", label: "Logs", icon: ScrollText },
  { key: "problems", label: "Problems", icon: TriangleAlert },
  { key: "results", label: "Output", icon: Braces },
  { key: "env", label: "Dev .env", icon: KeyRound },
];

export default function ConsoleTabs({
  active,
  onSelect,
  /** Per-tab counts, shown as a badge. Absent or 0 renders no badge. */
  counts,
  /** Appended to the Logs label while a runner is live. */
  running,
}: {
  active: ConsoleTab;
  onSelect(tab: ConsoleTab): void;
  counts?: Partial<Record<ConsoleTab, number>>;
  running?: boolean;
}) {
  return (
    <div className="flex items-center gap-0.5">
      {TABS.map(({ key, label, icon: Icon }) => {
        const count = counts?.[key] ?? 0;
        const selected = active === key;
        // Problems is the one tab that should draw the eye when it has something to say.
        const alarming = key === "problems" && count > 0;
        return (
          <button
            key={key}
            type="button"
            aria-pressed={selected}
            onClick={(e) => {
              e.stopPropagation();
              onSelect(key);
            }}
            className={`flex items-center gap-1.5 rounded px-2 py-0.5 text-xs font-medium tracking-tight transition-colors ${
              selected
                ? "bg-black/[0.06] text-zinc-800 dark:bg-white/10 dark:text-zinc-100"
                : "text-zinc-500 hover:text-zinc-700 dark:hover:text-zinc-300"
            }`}
          >
            <Icon
              size={13}
              className={alarming ? "text-rose-500 dark:text-rose-400" : undefined}
              aria-hidden
            />
            {label}
            {key === "logs" && running && " — running"}
            {count > 0 && (
              <span
                className={`rounded-full px-1.5 text-[10px] leading-4 tabular-nums ${
                  alarming
                    ? "bg-rose-500/15 text-rose-600 dark:text-rose-400"
                    : "bg-black/[0.06] text-zinc-500 dark:bg-white/10 dark:text-zinc-400"
                }`}
              >
                {count}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}
