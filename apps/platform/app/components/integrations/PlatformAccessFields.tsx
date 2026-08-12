"use client";

import { Boxes, ChartNoAxesGantt } from "lucide-react";

/**
 * The per-deployment platform-access grants, shared by the deploy and rollout
 * dialogs so the two cannot describe the same settings differently.
 *
 * These say what a deployment's flows are *meant* to reach. That is a smaller claim
 * than it sounds today — see the notes on each — but it is the claim a future access
 * model reads, so the declarations are worth collecting now rather than
 * reconstructing later for every deployment that already exists.
 *
 * Kept behind a disclosure because most integrations want neither: an app that
 * serves a webhook has no business reading the installation it runs on, and burying
 * the switches is how that stays the obvious default.
 */
export default function PlatformAccessFields({
  orchestratorApi,
  observabilityApi,
  busy,
  onOrchestratorApi,
  onObservabilityApi,
}: {
  orchestratorApi: boolean;
  observabilityApi: boolean;
  busy: boolean;
  onOrchestratorApi: (next: boolean) => void;
  onObservabilityApi: (next: boolean) => void;
}) {
  return (
    <details className="rounded-md border border-black/10 px-3 py-2 dark:border-white/10">
      <summary className="cursor-pointer text-xs font-semibold uppercase tracking-wide text-zinc-400">
        Advanced
      </summary>

      <div className="mt-3 space-y-3">
        <p className="text-xs text-zinc-400">
          What this deployment&apos;s flows may reach on the platform itself. Leave
          both off unless the integration is built to read its own installation.
        </p>

        <div>
          <label className="flex items-center gap-2 text-sm text-zinc-600 dark:text-zinc-300">
            <input
              type="checkbox"
              checked={orchestratorApi}
              disabled={busy}
              onChange={(e) => onOrchestratorApi(e.target.checked)}
              className="accent-sky-500"
            />
            <Boxes size={14} />
            Needs access to the orchestrator API
          </label>
          {orchestratorApi && (
            <p className="mt-2 text-xs text-zinc-400">
              Integrations, deployments, resources and secrets, at{" "}
              <code>ORCHESTRATOR_URL</code>. That address is already in every pod
              because the runtime needs it, so this records the intent rather than
              opening anything — a later release can restrict the API to the
              deployments that declared it.
            </p>
          )}
        </div>

        <div>
          <label className="flex items-center gap-2 text-sm text-zinc-600 dark:text-zinc-300">
            <input
              type="checkbox"
              checked={observabilityApi}
              disabled={busy}
              onChange={(e) => onObservabilityApi(e.target.checked)}
              className="accent-sky-500"
            />
            <ChartNoAxesGantt size={14} />
            Needs access to the observability API
          </label>
          {observabilityApi && (
            <p className="mt-2 text-xs text-amber-600 dark:text-amber-400">
              Injects <code>LOGS_URL</code>, the stored logs and traces of{" "}
              <strong>every</strong> deployment on this installation — not just this
              one. Captured request bodies are readable through it, so grant it to
              integrations you would trust with the Traces view.
            </p>
          )}
        </div>
      </div>
    </details>
  );
}
