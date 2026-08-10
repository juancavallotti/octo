"use client";

import { useEffect, useState } from "react";
import { GitCompareArrows, Server, X } from "lucide-react";
import type {
  Deployment,
  EnvBindingInput,
  Snapshot,
} from "@/app/model/orchestrator";
import { INPUT } from "./inputStyles";
import Field from "./Field";
import RolloutForm from "./RolloutForm";

/**
 * Modal that rolls a live deployment over to a version and edits its environment in
 * one step. It reuses the deploy modal's env editor (seeded from the deployment's
 * current bindings, so the operator can extend or change them) and the header's
 * version picker, and works two ways:
 *
 *  - `versionMode="pick"`  → choose an existing tag (roll to it) or Current (tag the
 *    working copy first, then roll). Used by the integrations deployments panel.
 *  - `versionMode="new-tag"` → always cut a new tag from the working copy. Used by
 *    the editor's Deploy button, which ships the on-screen definition.
 *
 * With more than one candidate deployment it also lets the operator pick which one to
 * upgrade. The parent owns save/createSnapshot/rollout (so it can refresh and surface
 * errors); this owns the form and reports the chosen target + env on submit.
 */

/** What the modal submits: which deployment to upgrade, the target version (an
 * existing snapshot or a new tag to cut), the full desired env (replaces the
 * deployment's stored bindings), and the desired tracing setting. */
export interface RolloutSubmit {
  deploymentId: string;
  snapshotId?: string;
  newTag?: string;
  env: Record<string, EnvBindingInput>;
  tracing: boolean;
}

export default function RolloutModal({
  integrationId,
  integrationName,
  deployments,
  snapshots,
  deployedTags,
  versionMode,
  actionLabel = "Roll out",
  busy,
  error,
  onSubmit,
  onClose,
}: {
  integrationId: string;
  integrationName: string;
  /** Candidate deployments to upgrade; one is fixed, several show a selector. */
  deployments: Deployment[];
  /** The integration's tags, for the version picker (pick mode) and tag suggestion. */
  snapshots: Snapshot[];
  /** Tags currently deployed, for the version picker's "deployed" hint. */
  deployedTags?: ReadonlySet<string>;
  versionMode: "pick" | "new-tag";
  /** Verb for the header and submit button (e.g. "Roll out", "Deploy"). */
  actionLabel?: string;
  busy: boolean;
  error: string | null;
  onSubmit: (input: RolloutSubmit) => void;
  onClose: () => void;
}) {
  const [selectedId, setSelectedId] = useState(deployments[0]?.id ?? "");
  const selected =
    deployments.find((d) => d.id === selectedId) ?? deployments[0];

  // Close on Escape, mirroring the deploy modal.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !busy) onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [busy, onClose]);

  if (!selected) return null;

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={`${actionLabel} ${integrationName}`}
      onMouseDown={() => !busy && onClose()}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4 backdrop-blur-sm"
    >
      <div
        onMouseDown={(e) => e.stopPropagation()}
        className="flex w-full max-w-md flex-col overflow-hidden rounded-xl border border-black/10 bg-white shadow-xl dark:border-white/10 dark:bg-zinc-900"
      >
        <header className="flex items-center gap-2 border-b border-black/10 px-4 py-3 dark:border-white/10">
          <GitCompareArrows size={16} className="text-sky-500" />
          <h3 className="min-w-0 flex-1 truncate text-sm font-semibold">
            {actionLabel} {integrationName}
          </h3>
          <button
            type="button"
            aria-label="Close"
            onClick={onClose}
            disabled={busy}
            className="rounded p-1 text-zinc-400 transition-colors hover:bg-black/[0.06] hover:text-zinc-700 disabled:opacity-50 dark:hover:bg-white/[0.08] dark:hover:text-zinc-200"
          >
            <X size={16} />
          </button>
        </header>

        {deployments.length > 1 && (
          <div className="border-b border-black/10 px-4 py-3 dark:border-white/10">
            <Field
              label="Deployment"
              hint="Only the selected deployment is upgraded."
            >
              <div className="flex items-center gap-2">
                <Server size={14} className="shrink-0 text-zinc-400" />
                <select
                  value={selectedId}
                  disabled={busy}
                  onChange={(e) => setSelectedId(e.target.value)}
                  className={`${INPUT} w-full`}
                >
                  {deployments.map((d) => (
                    <option key={d.id} value={d.id}>
                      {d.id.slice(0, 8)}
                      {d.tag ? ` · ${d.tag}` : ""} ({d.status})
                    </option>
                  ))}
                </select>
              </div>
            </Field>
          </div>
        )}

        {/* Keyed by the deployment so switching it re-seeds env from that deployment. */}
        <RolloutForm
          key={selected.id}
          integrationId={integrationId}
          deployment={selected}
          snapshots={snapshots}
          deployedTags={deployedTags ?? new Set()}
          versionMode={versionMode}
          actionLabel={actionLabel}
          busy={busy}
          error={error}
          onSubmit={onSubmit}
          onClose={onClose}
        />
      </div>
    </div>
  );
}

