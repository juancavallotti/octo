"use client";

import Link from "next/link";
import type { ReactNode } from "react";
import type { AgentStatus } from "@/app/model/agent";

/**
 * The agent's state, as a panel with its actions in the header — the shape the
 * integration surfaces use, because this *is* an integration and reading it
 * differently would be the odd thing.
 *
 * Presentation only: the manager owns loading and the actions and passes them in.
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
export const BLOCKED_REASONS: Record<
  string,
  { text: string; href?: string; cta?: string }
> = {
  kubernetes: {
    text: "This orchestrator has no cluster access, so it cannot deploy anything. The agent runs as a normal deployment, so it needs the same in-cluster access every integration does.",
  },
  encryption: {
    text: "This orchestrator has no encryption key, so it cannot read back the provider key the agent needs. Set kv.encryptionKey in the Helm values.",
  },
  llm_key: {
    text: "No LLM provider key is stored, so there is no model for the agent to reason with.",
    // The provider is now the section directly above this card rather than another
    // page, so this scrolls rather than navigates. It is still a link and still
    // named, because the reason has to say where to go even when the answer is
    // "just up there".
    href: "#llm-heading",
    cta: "Configure the LLM provider",
  },
};

/** One label/value row, with the value in mono when it is an identifier. */
function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-4 text-xs">
      <span className="shrink-0 text-zinc-500">{label}</span>
      <span className="min-w-0 truncate text-right text-zinc-700 dark:text-zinc-300">
        {children}
      </span>
    </div>
  );
}

export default function AgentStatusCard({
  status,
  actions,
  footer,
}: {
  status: AgentStatus;
  /** The buttons for this state, rendered at the top right of the panel. */
  actions: ReactNode;
  /**
   * Settings that belong to this deployment, rendered under a rule at the foot of
   * the panel. Falsy renders nothing, so a caller can pass a condition directly.
   */
  footer?: ReactNode;
}) {
  const blocked = status.blocked ? BLOCKED_REASONS[status.blocked] : undefined;

  return (
    <div className="mt-4 overflow-hidden rounded-lg border border-black/10 dark:border-white/10">
      {/* Badges left, actions right, on one line — the integration header's
          arrangement, so the two read the same way. */}
      <header className="flex flex-wrap items-center gap-2 border-b border-black/10 px-3 py-2 dark:border-white/10">
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
        <div className="ml-auto flex shrink-0 items-center gap-1.5">
          {actions}
        </div>
      </header>

      <div className="flex flex-col gap-3 px-3 py-3">
        {/* The blocked reason sits above the detail: it is why the buttons are
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

        {status.integrationId ? (
          <div className="flex flex-col gap-1.5">
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
          </div>
        ) : (
          <p className="text-xs text-zinc-500">
            Installing creates him as an integration, publishes a version from
            the bundle this orchestrator ships, and deploys him with no external
            route.
          </p>
        )}

        {/* Negative margins so the rule spans the panel rather than the padding
            box, which is what makes it read as part of the card. */}
        {footer && (
          <div className="-mx-3 -mb-3 border-t border-black/10 px-3 py-3 dark:border-white/10">
            {footer}
          </div>
        )}
      </div>
    </div>
  );
}
