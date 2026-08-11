"use client";

import Link from "next/link";
import type { AgentStatus } from "@/app/model/agent";

/**
 * What the agent's status looks like on screen. Presentation only — the manager
 * owns loading and the actions, so this stays a function of one status object.
 */

const STATE_LABELS: Record<string, string> = {
  not_installed: "Not installed",
  installed: "Installed, not running",
  deployed: "Running",
  update_available: "Update available",
  failed: "Failed",
};

const STATE_STYLES: Record<string, string> = {
  not_installed: "bg-zinc-500/15 text-zinc-600 dark:text-zinc-400",
  installed: "bg-amber-500/15 text-amber-600 dark:text-amber-400",
  deployed: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400",
  update_available: "bg-sky-500/15 text-sky-600 dark:text-sky-400",
  failed: "bg-red-500/15 text-red-600 dark:text-red-400",
};

/**
 * Why an install cannot proceed, said in the terms of the thing that needs fixing
 * rather than the state that is missing. Each one is somebody's next action, so
 * each names where to take it.
 */
export const BLOCKED_REASONS: Record<string, { text: string; href?: string; cta?: string }> = {
  kubernetes: {
    text: "This orchestrator has no cluster access, so it cannot deploy anything. The agent runs as a normal deployment, so it needs the same in-cluster access every integration does.",
  },
  encryption: {
    text: "This orchestrator has no encryption key, so it cannot read back the provider key the agent needs. Set kv.encryptionKey in the Helm values.",
  },
  llm_key: {
    text: "No LLM provider key is stored, so there is no model for the agent to reason with.",
    href: "/platform/admin/llm",
    cta: "Configure the LLM provider",
  },
};

/** One label/value row, with the value in mono when it is an identifier. */
function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-4 text-xs">
      <span className="shrink-0 text-zinc-500">{label}</span>
      <span className="min-w-0 truncate text-right text-zinc-700 dark:text-zinc-300">
        {children}
      </span>
    </div>
  );
}

export default function AgentStatusCard({ status }: { status: AgentStatus }) {
  const blocked = status.blocked ? BLOCKED_REASONS[status.blocked] : undefined;

  return (
    <div className="mt-4 flex flex-col gap-3 rounded-lg border border-black/10 p-4 dark:border-white/10">
      <div className="flex items-center gap-2">
        <span
          className={`rounded-full px-2 py-0.5 text-xs font-medium ${
            STATE_STYLES[status.state] ?? STATE_STYLES.not_installed
          }`}
        >
          {STATE_LABELS[status.state] ?? status.state}
        </span>
        {status.tracing && (
          <span className="rounded-full bg-violet-500/15 px-2 py-0.5 text-xs font-medium text-violet-600 dark:text-violet-400">
            Tracing on
          </span>
        )}
        {status.edited && (
          <span className="rounded-full bg-amber-500/15 px-2 py-0.5 text-xs font-medium text-amber-600 dark:text-amber-400">
            Edited
          </span>
        )}
      </div>

      {/* The blocked reason sits above the detail: it is why the buttons below are
          disabled, so reading it first is the point. */}
      {blocked && (
        <p className="rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-xs text-amber-600 dark:text-amber-400">
          {blocked.text}
          {blocked.href && (
            <>
              {" "}
              <Link href={blocked.href} className="font-medium underline">
                {blocked.cta}
              </Link>
              .
            </>
          )}
        </p>
      )}

      {status.state === "failed" && status.reason && (
        <p className="rounded-md border border-red-500/30 bg-red-500/5 px-3 py-2 text-xs text-red-600 dark:text-red-400">
          {status.reason}
        </p>
      )}

      {status.integrationId && (
        <div className="flex flex-col gap-1.5 border-t border-black/5 pt-3 dark:border-white/5">
          <Row label="Version">
            <code className="font-mono">{status.installedTag || "—"}</code>
          </Row>
          {status.internalUrl && (
            <Row label="Internal address">
              <code className="font-mono">{status.internalUrl}</code>
            </Row>
          )}
          {status.deploymentStatus && (
            <Row label="Deployment">
              <span className="capitalize">{status.deploymentStatus}</span>
            </Row>
          )}
          <Row label="Definition">
            {/* The agent is an ordinary integration, and this link is the proof —
                it opens in the same editor as anything else. */}
            <Link
              href={`/platform/i/${status.integrationId}`}
              className="font-medium text-sky-600 hover:underline dark:text-sky-400"
            >
              Open in the editor
            </Link>
          </Row>
        </div>
      )}
    </div>
  );
}
