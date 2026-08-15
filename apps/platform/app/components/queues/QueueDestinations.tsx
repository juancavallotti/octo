"use client";

import { useState } from "react";
import { ChevronRight, Layers } from "lucide-react";
import type { QueueDestination } from "@/app/model/queues";
import { SubscribersTable, num } from "./QueueViews";

/**
 * The queue destinations: one expandable row per subject clients consume from.
 * Collapsed, a row shows the queue name (and its deployment), subscription count,
 * and delivered messages; expanded, it shows the clients consuming it. The subject
 * doubles as the queue group for platform queues, so the subscription count is how
 * many competing consumers share the load.
 */
export default function QueueDestinations({
  destinations,
}: {
  destinations: QueueDestination[];
}) {
  return (
    <div className="overflow-hidden rounded-xl border border-black/10 dark:border-white/10">
      {destinations.map((d) => (
        <DestinationRow key={d.subject} dest={d} />
      ))}
    </div>
  );
}

function DestinationRow({ dest }: { dest: QueueDestination }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="border-b border-black/5 last:border-0 dark:border-white/5">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className="flex w-full items-center gap-3 px-3 py-2.5 text-left transition-colors hover:bg-black/[0.03] dark:hover:bg-white/[0.04]"
      >
        <ChevronRight
          size={15}
          className={`shrink-0 text-zinc-400 transition-transform ${
            open ? "rotate-90" : ""
          }`}
        />
        <Layers size={15} className="shrink-0 text-zinc-400" />
        <span className="min-w-0 flex-1">
          <span
            className="block truncate text-sm font-medium"
            title={dest.subject}
          >
            {dest.name}
          </span>
          {dest.deployment && (
            <span className="block truncate text-xs text-zinc-500 dark:text-zinc-400">
              {dest.deployment}
            </span>
          )}
        </span>
        {dest.queue && (
          <span className="hidden shrink-0 rounded bg-black/[0.05] px-1.5 py-0.5 text-xs text-zinc-500 sm:inline dark:bg-white/[0.08] dark:text-zinc-400">
            queue
          </span>
        )}
        <span className="shrink-0 text-xs tabular-nums text-zinc-500">
          {num(dest.subscriptions)} sub{dest.subscriptions === 1 ? "" : "s"}
        </span>
        <span className="shrink-0 text-xs tabular-nums text-zinc-500">
          {num(dest.msgs)} msg{dest.msgs === 1 ? "" : "s"}
        </span>
      </button>

      {open && (
        <div className="border-t border-black/5 bg-black/[0.015] p-3 dark:border-white/5 dark:bg-white/[0.02]">
          <SubscribersTable subscribers={dest.subscribers} />
        </div>
      )}
    </div>
  );
}
