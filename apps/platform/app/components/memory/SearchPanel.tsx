"use client";

import { useState } from "react";
import { Search } from "lucide-react";
import type { MemoryHit } from "@/app/model/agentMemory";
import { SearchRanking } from "./SearchRanking";

/**
 * Search across an agent's conversations and remembered facts.
 *
 * Its own tab rather than a box above the conversation list, which is where it
 * started. There it pushed the list and the transcript down the page whenever it
 * had results, so finding something cost you sight of everything else; and the
 * results deserve the width, since a hit is a line of somebody's conversation
 * rather than a key.
 *
 * The ranking line lives here too, for the same reason it exists at all: whether
 * search ranks by meaning or by words is a fact about *this* box, and it is the
 * first thing worth knowing when a result looks wrong.
 *
 * It does not label individual results as semantic or textual. The store decides
 * that per query — semantic when embeddings are configured and have something to
 * match, text otherwise — and mid-backfill two searches a second apart can be
 * answered by different indexes. Labelling each hit would be honest and useless.
 */
export function SearchPanel({
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
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 border-b border-black/10 px-4 py-3 dark:border-white/10">
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
            className="min-w-0 flex-1 rounded-md border border-black/10 bg-transparent px-3 py-1.5 text-sm dark:border-white/15"
          />
          <button
            type="submit"
            className="inline-flex shrink-0 items-center gap-1.5 rounded-md border border-black/10 px-3 py-1.5 text-sm dark:border-white/10"
          >
            <Search className="h-4 w-4" aria-hidden />
            Search
          </button>
        </form>
        <SearchRanking />
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {hits === null ? (
          // Nothing asked, so nothing shown. An empty results frame sitting under
          // the box would say "no matches" for a search nobody has run.
          <div className="flex h-full items-center justify-center p-8">
            <p className="max-w-sm text-center text-sm text-zinc-500 dark:text-zinc-400">
              Search this agent&rsquo;s conversations and the facts it keeps about people.
            </p>
          </div>
        ) : hits.length === 0 ? (
          <div className="flex h-full items-center justify-center p-8">
            <p className="text-sm text-zinc-500 dark:text-zinc-400">
              Nothing stored matches that.
            </p>
          </div>
        ) : (
          <ul className="flex flex-col">
            {hits.map((hit, i) => (
              <li
                key={`${hit.kind}-${hit.threadKey ?? hit.name}-${hit.seq ?? i}`}
                className="border-b border-black/5 last:border-0 dark:border-white/5"
              >
                <button
                  type="button"
                  disabled={!hit.threadKey}
                  onClick={() => hit.threadKey && onOpen(hit.threadKey)}
                  className="w-full px-4 py-3 text-left transition-colors enabled:hover:bg-black/[0.03] disabled:cursor-default dark:enabled:hover:bg-white/[0.04]"
                >
                  <span className="block text-xs uppercase tracking-wide text-zinc-500 dark:text-zinc-400">
                    {hit.kind === "user" ? `remembered · ${hit.name}` : "conversation"}
                  </span>
                  <span className="mt-0.5 block text-sm">{hit.text}</span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
