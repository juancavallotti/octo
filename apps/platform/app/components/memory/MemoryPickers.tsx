"use client";

import type { MemoryAgent } from "@/app/model/agentMemory";
import type { Integration } from "@/app/model/orchestrator";
import { Field, INPUT } from "@/app/components/admin/fields";

/**
 * What to look at: an integration, then one of the agents that has stored
 * something under it.
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
    <div className="mt-4 grid gap-3 sm:grid-cols-2">
      <Field label="Integration">
        <select
          value={integrationId}
          onChange={(e) => onIntegrationChange(e.target.value)}
          className={`${INPUT} w-full`}
        >
          <option value="">Choose an integration…</option>
          {integrations.map((i) => (
            <option key={i.id} value={i.id}>
              {i.name}
            </option>
          ))}
        </select>
      </Field>

      <Field
        label="Agent"
        hint={
          integrationId && agents.length === 0
            ? "No agent under this integration has stored anything yet."
            : "The id the ai-agent block declares."
        }
      >
        <select
          value={agentId}
          disabled={agents.length === 0}
          onChange={(e) => onAgentChange(e.target.value)}
          className={`${INPUT} w-full`}
        >
          <option value="">Choose an agent…</option>
          {agents.map((a) => (
            <option key={a.agentId} value={a.agentId}>
              {a.agentId} ({a.threadCount})
            </option>
          ))}
        </select>
      </Field>
    </div>
  );
}
