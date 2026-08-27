"use client";

import { Trash2 } from "lucide-react";
import type { UserMemory } from "@/app/model/agentMemory";

/**
 * What the agent chose to keep about the person in this conversation.
 *
 * Delete only — there is no edit. An operator rewriting what an agent believes
 * about somebody, with no audit trail and nothing in the conversation explaining
 * it, is a feature that should be asked for explicitly rather than fall out of a
 * viewer.
 */
export function UserMemoryList({
  userId,
  memories,
  onForget,
}: {
  userId: string;
  memories: UserMemory[];
  onForget: (name: string) => void;
}) {
  return (
    <div className="rounded-lg border border-black/10 dark:border-white/10">
      <div className="border-b border-black/10 px-4 py-3 dark:border-white/10">
        <h2 className="text-sm font-medium">Remembered about {userId}</h2>
        <p className="mt-0.5 text-xs text-zinc-500 dark:text-zinc-400">
          Carried into every later conversation with this person.
        </p>
      </div>
      {memories.length === 0 ? (
        <p className="px-4 py-3 text-sm text-zinc-500 dark:text-zinc-400">
          The agent has not kept anything about this person.
        </p>
      ) : (
        <ul className="flex flex-col">
          {memories.map((m) => (
            <li
              key={m.name}
              className="group flex items-start gap-2 border-b border-black/5 px-4 py-2 last:border-0 dark:border-white/5"
            >
              <div className="flex-1">
                <span className="block font-mono text-xs text-zinc-500 dark:text-zinc-400">
                  {m.name}
                </span>
                <span className="block text-sm">{m.value}</span>
              </div>
              <button
                type="button"
                onClick={() => onForget(m.name)}
                aria-label={`Forget ${m.name}`}
                className="mt-1 rounded p-1 text-zinc-400 opacity-0 transition hover:text-red-500 focus:opacity-100 group-hover:opacity-100"
              >
                <Trash2 className="h-4 w-4" />
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
