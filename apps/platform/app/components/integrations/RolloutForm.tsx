"use client";

import { useMemo, useState } from "react";
import { GitCompareArrows, Tag } from "lucide-react";
import type { Deployment, Snapshot } from "@/app/model/orchestrator";
import { suggestNextTag } from "@/app/model/tags";
import VersionMenu from "./VersionMenu";
import DeployEnvFields from "./DeployEnvFields";
import { useDeployEnv } from "./useDeployEnv";
import { useDeployOptions } from "./useDeployOptions";
import { INPUT } from "./inputStyles";
import Field from "./Field";
import TracingToggle from "./TracingToggle";
import type { RolloutSubmit } from "./RolloutModal";

/** The version + env form for a single deployment. Split out so it can be keyed by
 * the deployment: a change remounts it, re-seeding env from the new deployment. */
export default function RolloutForm({
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
  // Seeded from what the deployment is actually running, so the switch reads as
  // current state rather than as a blank instruction. The form is keyed by
  // deployment, so switching target re-seeds it.
  const [tracing, setTracing] = useState(deployment.tracing ?? false);

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

  const opts = useDeployOptions(integrationId, targetSnapshot);

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
      tracing,
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

        <Field
          label="Tracing"
          hint="Records every flow, block and model call, with what each one cost. For troubleshooting only — it significantly reduces throughput. Takes effect on this rollout, since the pods read it at startup."
        >
          <TracingToggle busy={busy} checked={tracing} onChange={setTracing} />
        </Field>

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
