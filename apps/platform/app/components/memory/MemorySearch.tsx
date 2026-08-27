"use client";

import { useState } from "react";
import { Search } from "lucide-react";
import type { MemoryHit } from "@/app/model/agentMemory";
import { INPUT } from "@/app/components/admin/fields";

/**
 * Search across an agent's conversations and remembered facts.
 *
 * It does not say whether the ranking was semantic or textual, deliberately. The
 * store decides that per query — semantic when an embedding provider is
 * configured and has something to match against, text otherwise — and mid-backfill
 * two searches a second apart can be answered by different indexes. Labelling
 * each result with which one found it would be honest and useless.
 */
export function MemorySearch({
  onSearch,
  hits,
  onOpen,
}: {
  onSearch: (text: string) => void;
  hits: MemoryHit[] | null;
  onOpen: (threadKey: string) => void;
}) {
  const [text, setText] = useState("");

  return (
    <div className="mt-4">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          onSearch(text);
        }}
        className="flex gap-2"
      >
        <input
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="Search what this agent remembers…"
          aria-label="Search agent memory"
          className={`${INPUT} flex-1`}
        />
        <button
          type="submit"
          className="inline-flex items-center gap-1.5 rounded-md border border-black/10 px-3 py-1.5 text-sm dark:border-white/10"
        >
          <Search className="h-4 w-4" />
          Search
        </button>
      </form>

      {hits !== null && (
        <div className="mt-3 rounded-lg border border-black/10 dark:border-white/10">
          {hits.length === 0 ? (
            <p className="px-4 py-3 text-sm text-zinc-500 dark:text-zinc-400">
              Nothing stored matches that.
            </p>
          ) : (
            <ul className="flex max-h-64 flex-col overflow-y-auto">
              {hits.map((hit, i) => (
                <li
                  key={`${hit.kind}-${hit.threadKey ?? hit.name}-${hit.seq ?? i}`}
                  className="border-b border-black/5 last:border-0 dark:border-white/5"
                >
                  <button
                    type="button"
                    disabled={!hit.threadKey}
                    onClick={() => hit.threadKey && onOpen(hit.threadKey)}
                    className="w-full px-4 py-2 text-left disabled:cursor-default"
                  >
                    <span className="block text-xs uppercase tracking-wide text-zinc-500 dark:text-zinc-400">
                      {hit.kind === "user" ? `remembered · ${hit.name}` : "conversation"}
                    </span>
                    <span className="mt-0.5 block truncate text-sm">{hit.text}</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
