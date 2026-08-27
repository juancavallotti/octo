"use client";

import { BrainCircuit } from "lucide-react";
import type { MemoryAgent } from "@/app/model/agentMemory";
import type { Integration } from "@/app/model/orchestrator";

/**
 * What to look at: an integration, then one of the agents that has stored
 * something under it.
 *
 * A single compact row rather than a pair of labelled fields, matching the object
 * store's toolbar — these two pickers scope everything on the page, so they belong
 * above the tabs and out of the way, not in a form-shaped block that reads as
 * something to fill in.
 *
 * The agent list is not read from any definition. An agent appears here once it
 * has actually recorded something, which is precisely when there is anything to
 * show — and it means an agent that was renamed leaves its old memory visible
 * under the old id rather than disappearing from the viewer along with its name.
 */
export function MemoryPickers({
  integrations,
  integrationId,
  onIntegrationChange,
  agents,
  agentId,
  onAgentChange,
}: {
  integrations: Integration[];
  integrationId: string;
  onIntegrationChange: (id: string) => void;
  agents: MemoryAgent[];
  agentId: string;
  onAgentChange: (id: string) => void;
}) {
  return (
    <div className="flex flex-wrap items-center gap-2 border-b border-black/10 px-4 py-2.5 dark:border-white/10">
      <BrainCircuit size={15} className="shrink-0 text-zinc-400" aria-hidden />
      <select
        value={integrationId}
        onChange={(e) => onIntegrationChange(e.target.value)}
        aria-label="Integration"
        className="min-w-0 max-w-md flex-1 rounded-md border border-black/10 bg-transparent px-2 py-1 text-sm dark:border-white/15"
      >
        <option value="">Select an integration…</option>
        {integrations.map((i) => (
          <option key={i.id} value={i.id}>
            {i.name}
          </option>
        ))}
      </select>

      {integrationId && (
        <>
          <span className="shrink-0 text-xs font-medium text-zinc-400">agent</span>
          <select
            value={agentId}
            disabled={agents.length === 0}
            onChange={(e) => onAgentChange(e.target.value)}
            aria-label="Agent"
            className="shrink-0 rounded-md border border-black/10 bg-transparent px-2 py-1 text-sm disabled:opacity-50 dark:border-white/15"
          >
            <option value="">Select an agent…</option>
            {agents.map((a) => (
              <option key={a.agentId} value={a.agentId}>
                {a.agentId} ({a.threadCount})
              </option>
            ))}
          </select>
          {agents.length === 0 && (
            <span className="shrink-0 text-xs text-zinc-500 dark:text-zinc-400">
              Nothing stored under this integration yet.
            </span>
          )}
        </>
      )}
    </div>
  );
}
