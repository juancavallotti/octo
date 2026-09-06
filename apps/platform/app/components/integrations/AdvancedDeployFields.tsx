"use client";

import { Boxes, ChartNoAxesGantt, Terminal } from "lucide-react";

/**
 * The settings almost no deployment needs, shared by the deploy and rollout
 * dialogs so the two cannot describe the same ones differently.
 *
 * Two kinds live here and they are not the same kind of claim. The platform-access
 * grants say what a deployment's flows are *meant* to reach — a smaller claim than
 * it sounds today, but the one a future access model reads, so collecting the
 * declarations now beats reconstructing them later for every deployment that
 * already exists. The runner says what its pods actually ARE, and takes effect
 * immediately.
 *
 * All of it sits behind a disclosure because the default is right for almost
 * everything: an app that serves a webhook has no business reading the installation
 * it runs on, and needs no shell to do its job. Burying these is how that stays the
 * obvious default.
 */
export default function AdvancedDeployFields({
  orchestratorApi,
  observabilityApi,
  runner,
  busy,
  onOrchestratorApi,
  onObservabilityApi,
  onRunner,
}: {
  orchestratorApi: boolean;
  observabilityApi: boolean;
  /** "" or "standard" for the default runner; "agentic" for the heavier one. */
  runner: string;
  busy: boolean;
  onOrchestratorApi: (next: boolean) => void;
  onObservabilityApi: (next: boolean) => void;
  onRunner: (next: string) => void;
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
              Injects <code>OBSERVABILITY_URL</code>, the stored logs and traces of{" "}
              <strong>every</strong> deployment on this installation — not just this
              one. Captured request bodies are readable through it, so grant it to
              integrations you would trust with the Traces view.
            </p>
          )}
        </div>

        <div className="border-t border-black/10 pt-3 dark:border-white/10">
          <label className="flex items-center gap-2 text-sm text-zinc-600 dark:text-zinc-300">
            <input
              type="checkbox"
              checked={runner === "agentic"}
              disabled={busy}
              onChange={(e) => onRunner(e.target.checked ? "agentic" : "")}
              className="accent-sky-500"
            />
            <Terminal size={14} />
            Run on the agentic runner
          </label>
          {runner === "agentic" ? (
            <p className="mt-2 text-xs text-amber-600 dark:text-amber-400">
              A heavier image carrying a shell, <code>curl</code>, <code>jq</code>,
              the standalone <code>octo</code> CLI, <code>dolphin</code> and a
              writable <code>/workspace</code>. For an integration whose flows run
              local commands or test other flows.{" "}
              <strong>Treat it as privileged, not just bigger:</strong> a pod with a
              shell and a runtime it can point at a definition it just wrote can run
              anything this pod can reach, so the boundary is the pod rather than any
              allow list in the flow.
            </p>
          ) : (
            <p className="mt-2 text-xs text-zinc-400">
              The default image is distroless — one binary, no shell, nothing
              writable. Tick this only for an integration built to run local
              commands, and only if you trust its definition.
            </p>
          )}
        </div>
      </div>
    </details>
  );
}
