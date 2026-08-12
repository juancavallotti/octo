"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { Activity, Pencil, Trash2 } from "lucide-react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import {
  getAgentStatus,
  installAgent,
  rolloutAgent,
  setAgentTracing,
  uninstallAgent,
  type AgentStatus,
} from "@/app/model/agent";
import AgentStatusCard from "./AgentStatusCard";
import { IconAction, PrimaryAction, SecondaryButton } from "./fields";

/**
 * Install, update, trace and remove the platform agent.
 *
 * The agent is deployed as an ordinary integration, so most of what an operator
 * might want here already exists elsewhere: logs and scaling are on the deployment,
 * the definition is in the editor. What is left is the lifecycle, which is this
 * page, and the one setting that is not a deployment setting — tracing.
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
  const installed = Boolean(status?.integrationId);
  const deployed = Boolean(status?.deploymentId);

  const install = () => canAct && !blocked && run(() => installAgent());

  const rollout = async () => {
    if (!canAct || blocked) return;
    const ok = await confirm({
      title: "Roll out the update?",
      body: status?.edited
        ? "Publishing a new version freezes the integration's current definition first, so the changes made to this agent will be replaced by the version shipped with this orchestrator."
        : "The agent will be published as a new version and its pods replaced.",
      confirmLabel: "Roll out",
      danger: status?.edited,
    });
    if (ok) run(() => rolloutAgent());
  };

  const toggleTracing = () => {
    if (!canAct || !deployed) return;
    run(() => setAgentTracing(!status?.tracing));
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
                <>
                  {/* One primary action per state, in the sky the platform uses
                      for Deploy everywhere else. */}
                  {!installed && (
                    <PrimaryAction onClick={install} disabled={!canAct || blocked}>
                      Install
                    </PrimaryAction>
                  )}
                  {installed && !deployed && (
                    <PrimaryAction onClick={install} disabled={!canAct || blocked}>
                      Deploy
                    </PrimaryAction>
                  )}
                  {installed && status.updateAvailable && (
                    <PrimaryAction onClick={rollout} disabled={!canAct || blocked}>
                      Roll out update
                    </PrimaryAction>
                  )}
                  {installed &&
                    status.state === "failed" &&
                    !status.updateAvailable && (
                      <PrimaryAction onClick={rollout} disabled={!canAct || blocked}>
                        Redeploy
                      </PrimaryAction>
                    )}

                  {deployed && (
                    <IconAction
                      onClick={toggleTracing}
                      disabled={!canAct}
                      label={status.tracing ? "Turn tracing off" : "Turn tracing on"}
                      // Lit when on, so the toggle's state is legible without
                      // reading the badge beside it.
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
                      onClick={remove}
                      disabled={!canAct}
                      label="Remove the agent"
                      className="hover:bg-red-500/10 hover:text-red-500"
                    >
                      <Trash2 size={16} />
                    </IconAction>
                  )}
                </>
              }
            />

            {deployed && (
              <p className="mt-2 text-xs text-zinc-500">
                Turning tracing on or off replaces the agent&rsquo;s pods: the runtime
                reads the setting when it starts, so it cannot be changed in place.
                Traces then appear under Traces like any other integration&rsquo;s.
              </p>
            )}
          </>
        )}
      </div>
    </div>
  );
}
