"use client";

import Link from "next/link";
import { Activity, Pencil, Trash2 } from "lucide-react";
import type { AgentStatus } from "@/app/model/agent";
import { IconAction, PrimaryAction } from "./fields";

/**
 * The buttons on the agent's status card.
 *
 * Split from the manager because the manager is the state machine — load, run,
 * refresh, confirm — and this is the table of which control that state offers.
 * Reading either one used to mean scrolling past the other.
 */
export default function AgentActions({
  status,
  canAct,
  onInstall,
  onRollout,
  onToggleTracing,
  onRemove,
}: {
  status: AgentStatus;
  /** The page knows what is installed and is not mid-action. */
  canAct: boolean;
  onInstall: () => void;
  onRollout: () => void;
  onToggleTracing: () => void;
  onRemove: () => void;
}) {
  const blocked = Boolean(status.blocked);
  const installed = Boolean(status.integrationId);
  const deployed = Boolean(status.deploymentId);

  return (
    <>
      {/* One primary action per state, in the sky the platform uses for Deploy
          everywhere else. */}
      {!installed && (
        <PrimaryAction onClick={onInstall} disabled={!canAct || blocked}>
          Install
        </PrimaryAction>
      )}
      {installed && !deployed && (
        <PrimaryAction onClick={onInstall} disabled={!canAct || blocked}>
          Deploy
        </PrimaryAction>
      )}
      {/* One button, three names. All three call the same roll-out — republish the
          shipped bundle and replace the pods — so making them separate controls
          would only invite the question of which one to press. It is always offered
          once something is deployed: before, an agent edited into a state you wanted
          to undo had no way back, because the shipped digest had not moved and the
          button that would have fixed it was hidden for that reason. */}
      {installed && deployed && (
        <PrimaryAction onClick={onRollout} disabled={!canAct || blocked}>
          {status.updateAvailable
            ? "Roll out update"
            : status.state === "failed"
              ? "Redeploy"
              : "Reinstall from stock"}
        </PrimaryAction>
      )}

      {deployed && (
        <IconAction
          onClick={onToggleTracing}
          disabled={!canAct}
          label={status.tracing ? "Turn tracing off" : "Turn tracing on"}
          // Lit when on, so the toggle's state is legible without reading the
          // badge beside it.
          className={
            status.tracing
              ? "text-violet-500 hover:bg-violet-500/10"
              : undefined
          }
        >
          <Activity size={16} />
        </IconAction>
      )}

      {status.integrationId && (
        <Link
          href={`/platform/i/${status.integrationId}`}
          aria-label="Open the agent in the editor"
          title="Open in the editor"
          className="rounded-md p-1.5 text-zinc-400 transition-colors hover:bg-black/[0.06] hover:text-zinc-700 dark:hover:bg-white/10 dark:hover:text-zinc-200"
        >
          <Pencil size={16} />
        </Link>
      )}

      {installed && (
        <IconAction
          onClick={onRemove}
          disabled={!canAct}
          label="Remove the agent"
          className="hover:bg-red-500/10 hover:text-red-500"
        >
          <Trash2 size={16} />
        </IconAction>
      )}
    </>
  );
}
