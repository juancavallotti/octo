"use client";

import { useEffect, useMemo, useState } from "react";
import { GitCompareArrows, Server, Tag, X } from "lucide-react";
import {
  getDeployOptions,
  type Deployment,
  type DeployOptions,
  type EnvBindingInput,
  type Snapshot,
} from "@/app/model/orchestrator";
import { suggestNextTag } from "@/app/model/tags";
import VersionMenu from "./VersionMenu";
import DeployEnvFields from "./DeployEnvFields";
import { useDeployEnv } from "./useDeployEnv";
import Field from "./Field";

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

const INPUT =
  "rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/** What the modal submits: which deployment to upgrade, the target version (an
 * existing snapshot or a new tag to cut), and the full desired env (replaces the
 * deployment's stored bindings). */
export interface RolloutSubmit {
  deploymentId: string;
  snapshotId?: string;
  newTag?: string;
  env: Record<string, EnvBindingInput>;
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

/** The version + env form for a single deployment. Split out so it can be keyed by
 * the deployment: a change remounts it, re-seeding env from the new deployment. */
function RolloutForm({
  integrationId,
  deployment,
  snapshots,
  deployedTags,
  versionMode,
  actionLabel,
  busy,
  error,
  onSubmit,
  onClose,
}: {
  integrationId: string;
  deployment: Deployment;
  snapshots: Snapshot[];
  deployedTags: ReadonlySet<string>;
  versionMode: "pick" | "new-tag";
  actionLabel: string;
  busy: boolean;
  error: string | null;
  onSubmit: (input: RolloutSubmit) => void;
  onClose: () => void;
}) {
  // In pick mode default to the deployment's current version (an env-only re-rollout
  // is the least-surprising default); Current when it has no known tag. New-tag mode
  // always cuts a fresh tag, so there is no existing target.
  const [targetTag, setTargetTag] = useState<string | null>(() =>
    versionMode === "pick" &&
    deployment.tag &&
    snapshots.some((s) => s.tag === deployment.tag)
      ? deployment.tag
      : null,
  );
  const [newTag, setNewTag] = useState(() =>
    suggestNextTag(snapshots.map((s) => s.tag)),
  );
  const [opts, setOpts] = useState<DeployOptions | null>(null);

  const targetSnapshot = useMemo(
    () =>
      versionMode === "pick" && targetTag
        ? (snapshots.find((s) => s.tag === targetTag) ?? null)
        : null,
    [versionMode, targetTag, snapshots],
  );
  // Cutting a new tag freezes the working copy, so options come from the live
  // definition (no snapshotId); an existing target reads its frozen definition.
  const creatingNewTag = targetSnapshot === null;

  // Env editor seeded from the deployment's current bindings so the operator edits
  // rather than re-enters. Declared vars + provided-by-file keys follow the target.
  const {
    envVars,
    bindings,
    secretNames,
    setBinding,
    complete: envComplete,
    missingRequired,
    providedKeys,
    build: buildEnv,
  } = useDeployEnv(opts, deployment.env);

  // Load deploy options for the target version; on failure assume non-networked with
  // no declared env (the modal still lets the operator roll a version bump).
  useEffect(() => {
    let active = true;
    getDeployOptions(
      integrationId,
      targetSnapshot ? { snapshotId: targetSnapshot.id } : {},
    ).then(
      (o) => active && setOpts(o),
      () =>
        active &&
        setOpts({ networked: false, slugValid: false, slugAvailable: false }),
    );
    return () => {
      active = false;
    };
  }, [integrationId, targetSnapshot]);

  const canRollout =
    !busy &&
    opts !== null &&
    (!creatingNewTag || newTag.trim() !== "") &&
    envComplete;

  const submit = () => {
    if (!canRollout) return;
    onSubmit({
      deploymentId: deployment.id,
      ...(creatingNewTag
        ? { newTag: newTag.trim() }
        : { snapshotId: targetSnapshot!.id }),
      env: buildEnv(),
    });
  };

  return (
    <>
      <div className="flex max-h-[70vh] flex-col gap-5 overflow-y-auto px-4 py-4">
        {versionMode === "pick" ? (
          <Field
            label="Version"
            hint={
              creatingNewTag
                ? "Current tags the working copy first, then rolls to that frozen version."
                : "Rolls this deployment over to the selected tag's frozen definition."
            }
          >
            <VersionMenu
              snapshots={snapshots}
              deployedTags={deployedTags}
              value={targetTag}
              onChange={setTargetTag}
            />
            {creatingNewTag && (
              <div className="mt-2 flex items-center gap-2">
                <Tag size={14} className="shrink-0 text-zinc-400" />
                <input
                  value={newTag}
                  disabled={busy}
                  placeholder="e.g. v1.0.0"
                  onChange={(e) => setNewTag(e.target.value)}
                  className={`${INPUT} w-full`}
                />
              </div>
            )}
          </Field>
        ) : (
          <Field
            label="New version"
            hint="Ships the on-screen definition: tags the working copy, then rolls out."
          >
            <div className="flex items-center gap-2">
              <Tag size={14} className="shrink-0 text-zinc-400" />
              <input
                value={newTag}
                disabled={busy}
                placeholder="e.g. v1.0.0"
                onChange={(e) => setNewTag(e.target.value)}
                className={`${INPUT} w-full`}
              />
            </div>
          </Field>
        )}

        {opts === null ? (
          <p className="text-sm text-zinc-400">Loading options…</p>
        ) : (
          envVars.length > 0 && (
            <Field
              label="Environment"
              hint="Seeded from this deployment. Blank falls back to an .env file or default."
            >
              <DeployEnvFields
                envVars={envVars}
                bindings={bindings}
                secretNames={secretNames}
                providedKeys={providedKeys}
                busy={busy}
                onChange={setBinding}
              />
              {missingRequired.length > 0 && (
                <p className="mt-2 text-xs text-amber-600 dark:text-amber-400">
                  Provide a value or secret for:{" "}
                  <span className="font-mono">{missingRequired.join(", ")}</span>
                </p>
              )}
            </Field>
          )
        )}

        {error && <p className="text-sm text-red-500">{error}</p>}
      </div>

      <footer className="flex justify-end gap-2 border-t border-black/10 px-4 py-3 dark:border-white/10">
        <button
          type="button"
          onClick={onClose}
          disabled={busy}
          className="rounded-md px-3 py-1 text-sm text-zinc-600 transition-colors hover:bg-black/[0.06] disabled:opacity-50 dark:text-zinc-300 dark:hover:bg-white/[0.08]"
        >
          Cancel
        </button>
        <button
          type="button"
          onClick={submit}
          disabled={!canRollout}
          className="inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white transition-colors hover:bg-sky-500 disabled:opacity-50"
        >
          <GitCompareArrows size={14} />
          {actionLabel}
        </button>
      </footer>
    </>
  );
}
