"use client";

import { Workflow } from "lucide-react";
import type { Integration } from "@/app/model/orchestrator";

/** The middle column: the selected bucket's integrations, selectable into the detail panel. */
interface Props {
  integrations: Integration[];
  selectedId: string | null;
  onSelect: (id: string) => void;
}

export default function IntegrationList({
  integrations,
  selectedId,
  onSelect,
}: Props) {
  return (
    <div className="flex w-72 shrink-0 flex-col border-r border-black/10 dark:border-white/10">
      {integrations.length === 0 ? (
        <p className="px-4 py-4 text-sm text-zinc-400">No integrations here.</p>
      ) : (
        <ul className="min-h-0 flex-1 overflow-y-auto py-1">
          {integrations.map((i) => (
            <li key={i.id}>
              <button
                type="button"
                onClick={() => onSelect(i.id)}
                className={`flex w-full items-center gap-3 px-4 py-2 text-left ${
                  selectedId === i.id
                    ? "bg-sky-500/10"
                    : "hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
                }`}
              >
                <span
                  className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-md ${
                    selectedId === i.id
                      ? "bg-sky-500/15 text-sky-600 dark:text-sky-400"
                      : "bg-black/[0.04] text-zinc-500 dark:bg-white/[0.06] dark:text-zinc-400"
                  }`}
                >
                  <Workflow size={16} />
                </span>
                <span className="flex min-w-0 flex-col gap-0.5">
                  <span className="truncate text-sm font-medium">{i.name}</span>
                  <span className="text-xs text-zinc-400">
                    {new Date(i.lastUpdated).toLocaleDateString()}
                  </span>
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
