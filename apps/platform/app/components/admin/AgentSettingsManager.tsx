"use client";

import { useCallback, useEffect, useState } from "react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import {
  getAgentStatus,
  installAgent,
  rolloutAgent,
  setAgentMaxIterations,
  setAgentTracing,
  uninstallAgent,
  type AgentStatus,
} from "@/app/model/agent";
import AgentActions from "./AgentActions";
import AgentStatusCard from "./AgentStatusCard";
import AgentTurnLimit from "./AgentTurnLimit";
import { SecondaryButton } from "./fields";

/**
 * Install, update, trace and remove the platform agent.
 *
 * The agent is deployed as an ordinary integration, so most of what an operator
 * might want here already exists elsewhere: logs and scaling are on the deployment,
 * the definition is in the editor. What is left is the lifecycle, which is this
 * page, and the two settings that are not deployment settings — tracing, and how
 * many turns one of his runs may take.
 *
 * The buttons live in AgentActions and the turn limit in AgentTurnLimit: this file
 * is the state machine — load, run, refresh, confirm — and reading it used to mean
 * scrolling past a screenful of JSX to find it.
 */
export default function AgentSettingsManager() {
  const confirm = useConfirm();
  const [status, setStatus] = useState<AgentStatus | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Two failure modes with different remedies, so they are tracked separately: an
  // action that failed leaves a status on screen to act on again, whereas a load
  // that failed leaves nothing at all and needs a way to ask once more.
  const [loadFailed, setLoadFailed] = useState(false);

  const load = useCallback(
    () =>
      getAgentStatus().then(
        (next) => {
          setStatus(next);
          setLoadFailed(false);
        },
        (e) => {
          // Dropping the status rather than keeping the last one matters most
          // *after* an action: an install that succeeded and then failed to
          // refresh would otherwise leave the pre-install card on screen, with
          // buttons offering to install an agent that now exists. A status that
          // cannot be read is unknown, not unchanged.
          setStatus(null);
          setError((e as Error).message);
          setLoadFailed(true);
        },
      ),
    [],
  );

  useEffect(() => {
    load();
  }, [load]);

  /** Run a mutation, then refresh; surface failures inline. */
  const run = useCallback(
    async (fn: () => Promise<unknown>) => {
      setBusy(true);
      setError(null);
      try {
        await fn();
        await load();
      } catch (e) {
        setError((e as Error).message);
      } finally {
        setBusy(false);
      }
    },
    [load],
  );

  /** Ask for the status again after a failed load. */
  const retry = () => {
    setBusy(true);
    setError(null);
    load().finally(() => setBusy(false));
  };

  // Nothing is actionable before the first load resolves: every button below is a
  // decision about what is already installed, and a fast click on a page that does
  // not yet know would be a guess.
  const canAct = status !== null && !busy;
  const blocked = Boolean(status?.blocked);
  const deployed = Boolean(status?.deploymentId);

  const install = () => canAct && !blocked && run(() => installAgent());

  const rollout = async () => {
    if (!canAct || blocked) return;
    const ok = await confirm({
      title: status?.updateAvailable ? "Roll out the update?" : "Reinstall from stock?",
      // The edited case used to read as a threat, which overstated it: the live
      // definition is frozen as its own version before it is replaced, so the edits
      // are recoverable rather than lost. Saying which version, and that it can be
      // deployed again, is the difference between a warning and a dead end.
      body: status?.edited
        ? "The current definition is frozen as its own version first — look for an agent-edited-… tag under Versions — and then replaced by the one shipped with this orchestrator. Your changes stop running, but you can read or deploy that version afterwards."
        : "The agent is published as a version and its pods replaced with the definition shipped by this orchestrator.",
      confirmLabel: status?.updateAvailable ? "Roll out" : "Reinstall",
      danger: status?.edited,
    });
    if (ok) run(() => rolloutAgent());
  };

  const toggleTracing = () => {
    if (!canAct || !deployed) return;
    run(() => setAgentTracing(!status?.tracing));
  };

  const applyTurns = (limit: number) => {
    if (!canAct || !deployed) return;
    run(() => setAgentMaxIterations(limit));
  };

  const remove = async () => {
    if (!canAct) return;
    const ok = await confirm({
      title: "Remove the agent?",
      body: "The deployment is removed and the chat panel stops working. The integration is kept, so any changes made to the agent survive and installing again reuses them.",
      confirmLabel: "Remove",
      danger: true,
    });
    if (ok) run(() => uninstallAgent(false));
  };

  return (
    <div className="flex h-full flex-col overflow-y-auto px-6 py-5">
      <div className="mx-auto w-full max-w-2xl">
        <h1 className="text-lg font-semibold">Platform agent</h1>
        <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
          Dr. Octo answers questions about this installation and helps build
          integrations. He is deployed as an ordinary integration from a definition
          this orchestrator ships — so you can open him in the editor and change the
          prompt, the tools or the skills, and the usual logs, traces and scaling all
          work on him.
        </p>

        {error && <p className="mt-3 text-sm text-red-500">{error}</p>}

        {status === null && loadFailed ? (
          <SecondaryButton onClick={retry} disabled={busy}>
            Try again
          </SecondaryButton>
        ) : status === null ? (
          <p className="mt-4 text-sm text-zinc-500">Loading…</p>
        ) : (
          <>
            <AgentStatusCard
              status={status}
              actions={
                <AgentActions
                  status={status}
                  canAct={canAct}
                  onInstall={install}
                  onRollout={rollout}
                  onToggleTracing={toggleTracing}
                  onRemove={remove}
                />
              }
            />

            {deployed && (
              <p className="mt-2 text-xs text-zinc-500">
                Turning tracing on or off replaces the agent&rsquo;s pods: the runtime
                reads the setting when it starts, so it cannot be changed in place.
                Traces then appear under Traces like any other integration&rsquo;s.
              </p>
            )}

            {/* Keyed on what is in force so a successful apply remounts the field
                seeded from what came back, rather than syncing it in an effect
                that would fight anyone typing mid-roll-out. */}
            {deployed && (
              <AgentTurnLimit
                key={status.maxIterations ?? "default"}
                value={status.maxIterations}
                disabled={!canAct}
                onApply={applyTurns}
              />
            )}
          </>
        )}
      </div>
    </div>
  );
}
