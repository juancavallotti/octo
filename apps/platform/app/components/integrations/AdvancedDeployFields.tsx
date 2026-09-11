"use client";

import { ShieldCheck, Terminal } from "lucide-react";
import {
  DEPLOYMENT_ACCESS,
  type DeploymentAccess,
} from "@/app/model/orchestratorTypes";

/**
 * The settings almost no deployment needs, shared by the deploy and rollout
 * dialogs so the two cannot describe the same ones differently.
 *
 * Two settings, and they are not the same kind of claim. Access says what this
 * deployment's own token opens on the platform it runs on. The runner says what
 * its pods actually ARE.
 *
 * Both sit behind a disclosure because the defaults are right for almost
 * everything: an app that serves a webhook has no business reading the
 * installation it runs on, and needs no shell to do its job. Burying these is
 * how that stays the obvious default.
 */
export default function AdvancedDeployFields({
  access,
  runner,
  busy,
  onAccess,
  onRunner,
}: {
  access: DeploymentAccess;
  /** "" or "standard" for the default runner; "agentic" for the heavier one. */
  runner: string;
  busy: boolean;
  onAccess: (next: DeploymentAccess) => void;
  onRunner: (next: string) => void;
}) {
  const chosen = DEPLOYMENT_ACCESS.find((a) => a.value === access);

  return (
    <details className="rounded-md border border-black/10 px-3 py-2 dark:border-white/10">
      <summary className="cursor-pointer text-xs font-semibold uppercase tracking-wide text-zinc-400">
        Advanced
      </summary>

      <div className="mt-3 space-y-3">
        <div>
          <label className="flex items-center gap-2 text-sm text-zinc-600 dark:text-zinc-300">
            <ShieldCheck size={14} />
            What this deployment may reach
          </label>
          <select
            value={access}
            disabled={busy}
            aria-label="What this deployment may reach"
            onChange={(e) => onAccess(e.target.value as DeploymentAccess)}
            className="mt-2 w-full rounded-md border border-black/10 bg-transparent px-2 py-1 text-sm disabled:opacity-50 dark:border-white/15"
          >
            {DEPLOYMENT_ACCESS.map(({ value, label }) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
          <p
            className={`mt-2 text-xs ${
              access === "basic"
                ? "text-zinc-400"
                : "text-amber-600 dark:text-amber-400"
            }`}
          >
            {chosen?.detail}
          </p>
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
