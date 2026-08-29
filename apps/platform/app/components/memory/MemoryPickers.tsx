"use client";

import { BrainCircuit } from "lucide-react";
import type { MemoryAgent } from "@/app/model/agentMemory";
import type { Integration } from "@/app/model/orchestrator";
import { AppPicker } from "@/app/components/AppPicker";

/**
 * What to look at: an integration, then one of the agents that has stored
 * something under it.
 *
 * A single compact row rather than a pair of labelled fields — these two pickers
 * scope everything on the page, so they belong above the tabs and out of the way,
 * not in a form-shaped block that reads as something to fill in. The agent is an
 * accessory beside the integration rather than a second dropdown revealed by it:
 * it still needs an integration to mean anything, but that is a fact about the
 * list it offers, not a reason to hide the control.
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
  const selected = integrations.find((i) => i.id === integrationId) ?? null;

  return (
    <AppPicker<Integration>
      items={integrations}
      selected={selected}
      onSelect={(i) => onIntegrationChange(i.id)}
      toKey={(i) => i.id}
      toText={(i) => `${i.name} ${i.id}`}
      renderRow={(i) => <span className="text-sm">{i.name}</span>}
      renderValue={(i) => i.name}
      label="Integration"
      placeholder="Choose an integration…"
      empty="No integration exists yet."
      leading={<BrainCircuit size={15} className="shrink-0 text-zinc-400" aria-hidden />}
      accessory={
        integrationId && (
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
        )
      }
    />
  );
}
