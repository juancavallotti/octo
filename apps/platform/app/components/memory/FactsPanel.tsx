"use client";

import { UserRound } from "lucide-react";
import type { UserMemory } from "@/app/model/agentMemory";
import { UserMemoryList } from "./UserMemoryList";

/**
 * What the agent has chosen to keep about the people it talks to.
 *
 * Its own tab, addressed by person, because that is what a fact belongs to. It
 * used to appear only underneath an open conversation — which made sense (a
 * conversation names the person) and answered the wrong question: "what does this
 * agent know about Juan" is not a question about any one conversation, and
 * answering it meant opening conversations until you found one of theirs.
 *
 * The people offered are those on the conversations currently loaded. That is a
 * page of them rather than everyone the agent has ever spoken to — the listing is
 * cursor-paged and there is no separate route that enumerates people — so the
 * picker is described as who has been talked to recently rather than as a roster.
 */
export function FactsPanel({
  people,
  userId,
  onUserChange,
  memories,
  onForget,
}: {
  people: string[];
  userId: string;
  onUserChange: (id: string) => void;
  memories: UserMemory[];
  onForget: (name: string) => void;
}) {
  if (people.length === 0) {
    return (
      <div className="flex h-full items-center justify-center p-8">
        <p className="max-w-sm text-center text-sm text-zinc-500 dark:text-zinc-400">
          No conversation loaded here names a person, so there is nobody to have kept
          facts about. An agent records facts only when its definition gives it a
          <code className="mx-1 font-mono">userId</code>.
        </p>
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-center gap-2 border-b border-black/10 px-4 py-2.5 dark:border-white/10">
        <UserRound size={15} className="shrink-0 text-zinc-400" aria-hidden />
        <select
          value={userId}
          onChange={(e) => onUserChange(e.target.value)}
          aria-label="Person"
          className="min-w-0 max-w-md flex-1 rounded-md border border-black/10 bg-transparent px-2 py-1 text-sm dark:border-white/15"
        >
          <option value="">Select a person…</option>
          {people.map((p) => (
            <option key={p} value={p}>
              {p}
            </option>
          ))}
        </select>
        <span className="shrink-0 text-xs text-zinc-500 dark:text-zinc-400">
          people on the conversations loaded
        </span>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto p-4">
        {userId ? (
          <UserMemoryList userId={userId} memories={memories} onForget={onForget} />
        ) : (
          <p className="text-sm text-zinc-500 dark:text-zinc-400">
            Choose a person to see what this agent has kept about them.
          </p>
        )}
      </div>
    </div>
  );
}
