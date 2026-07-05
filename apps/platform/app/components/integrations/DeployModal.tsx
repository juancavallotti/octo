"use client";

import { useEffect, useState } from "react";
import { Globe, Rocket, Tag, X } from "lucide-react";
import {
  getDeployOptions,
  type DeployOptions,
  type EnvBindingInput,
  type Snapshot,
} from "@/app/model/orchestrator";
import { suggestNextTag } from "@/app/model/tags";
import SlugField from "./SlugField";
import DeployEnvFields from "./DeployEnvFields";
import { useDeployEnv } from "./useDeployEnv";
import Field from "./Field";

/**
 * Modal that collects per-deploy options and deploys. The version is decided
 * outside the modal (by the header's version picker), not chosen here:
 *
 *  - a tag is active  → deploy that frozen snapshot directly (version shown, fixed).
 *  - Current is active → tag the working copy first, then deploy it; the operator
 *    names the new tag here (prefilled with the suggested next version).
 *
 * It also holds scale (replicas) and, for an HTTP source, the address slug plus
 * optional external exposure. The parent owns the deploy call (so it can create the
 * tag, refresh, and surface errors); this owns the form and closes on success.
 */

const INPUT =
  "rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/** What the modal submits: either an existing snapshot to deploy, or a new tag to
 * cut from the working copy first. The parent resolves the two into a deployment. */
export interface DeploySubmit {
  snapshotId?: string;
  newTag?: string;
  replicas: number;
  slug?: string;
  expose?: "external";
  env?: Record<string, EnvBindingInput>;
}

export default function DeployModal({
  integrationId,
  integrationName,
  activeSnapshot,
  snapshots,
  busy,
  error,
  onSubmit,
  onClose,
}: {
  integrationId: string;
  integrationName: string;
  /** The version to deploy: a frozen tag, or null to tag-and-deploy the working copy. */
  activeSnapshot: Snapshot | null;
  /** All tags, to suggest the next version when tagging the working copy. */
  snapshots: Snapshot[];
  busy: boolean;
  error: string | null;
  onSubmit: (input: DeploySubmit) => void;
  onClose: () => void;
}) {
  const currentMode = activeSnapshot === null;
  const [replicas, setReplicas] = useState(1);
  const [expose, setExpose] = useState(false);
  const [slug, setSlug] = useState("");
  const [slugOk, setSlugOk] = useState(false);
  const [opts, setOpts] = useState<DeployOptions | null>(null);
  // The new tag to cut when deploying Current; prefilled with the suggested next
  // version. Unused in tag mode.
  const [newTag, setNewTag] = useState(() =>
    suggestNextTag(snapshots.map((s) => s.tag)),
  );
  // Environment-variable bindings (and the secret picker) live in a dedicated hook.
  const {
    envVars,
    bindings,
    secretNames,
    setBinding,
    complete: envComplete,
    missingRequired,
    providedKeys,
    build: buildEnv,
  } = useDeployEnv(opts);

  // Load deploy options for the active version: a tag reads its frozen definition,
  // Current the live working copy. Drives networked-ness, a free slug to prefill,
  // and the env vars to prompt for. On failure assume non-networked.
  useEffect(() => {
    let active = true;
    getDeployOptions(
      integrationId,
      activeSnapshot ? { snapshotId: activeSnapshot.id } : {},
    ).then(
      (o) => {
        if (!active) return;
        setOpts(o);
        setSlug(o.suggestedSlug ?? "");
      },
      () =>
        active &&
        setOpts({ networked: false, slugValid: false, slugAvailable: false }),
    );
    return () => {
      active = false;
    };
  }, [integrationId, activeSnapshot]);

  // Close on Escape, mirroring the editor's other overlays.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !busy) onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [busy, onClose]);

  const networked = opts?.networked ?? false;
  const canDeploy =
    !busy &&
    opts !== null &&
    (!currentMode || newTag.trim() !== "") &&
    (!networked || slugOk) &&
    envComplete;

  const submit = () => {
    if (!canDeploy) return;
    const env = buildEnv();
    onSubmit({
      ...(activeSnapshot
        ? { snapshotId: activeSnapshot.id }
        : { newTag: newTag.trim() }),
      replicas,
      ...(networked ? { slug: slug.trim() } : {}),
      ...(networked && expose ? { expose: "external" } : {}),
      ...(Object.keys(env).length ? { env } : {}),
    });
  };

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={`Deploy ${integrationName}`}
      onMouseDown={() => !busy && onClose()}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4 backdrop-blur-sm"
    >
      <div
        onMouseDown={(e) => e.stopPropagation()}
        className="flex w-full max-w-md flex-col overflow-hidden rounded-xl border border-black/10 bg-white shadow-xl dark:border-white/10 dark:bg-zinc-900"
      >
        <header className="flex items-center gap-2 border-b border-black/10 px-4 py-3 dark:border-white/10">
          <Rocket size={16} className="text-sky-500" />
          <h3 className="min-w-0 flex-1 truncate text-sm font-semibold">
            Deploy {integrationName}
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

        <div className="flex max-h-[70vh] flex-col gap-5 overflow-y-auto px-4 py-4">
          {currentMode ? (
            <Field
              label="New version"
              hint="Deploying Current tags the working copy first, then ships that frozen version."
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
          ) : (
            <Field
              label="Version"
              hint="Ships this tag's frozen definition. Pick a different version from the header."
            >
              <span className="inline-flex items-center gap-1.5 rounded-md bg-sky-500/10 px-2 py-1 text-sm font-medium text-sky-600 dark:text-sky-400">
                <Tag size={14} />
                {activeSnapshot.tag}
              </span>
            </Field>
          )}

          <Field label="Scale" hint="Runtime pods load-balanced behind the service.">
            <label className="flex items-center gap-2 text-sm text-zinc-600 dark:text-zinc-300">
              Replicas
              <input
                type="number"
                min={1}
                value={replicas}
                disabled={busy}
                onChange={(e) =>
                  setReplicas(Math.max(1, Number(e.target.value) || 1))
                }
                className={`${INPUT} w-20`}
              />
            </label>
          </Field>

          {opts === null ? (
            <p className="text-sm text-zinc-400">Loading options…</p>
          ) : networked ? (
            <Field
              label="Address"
              hint={`Reachable in-cluster at octo-int-${slug.trim() || "{slug}"}. Must be unique.`}
            >
              <SlugField
                integrationId={integrationId}
                value={slug}
                onChange={setSlug}
                expose={expose}
                busy={busy}
                onValidChange={setSlugOk}
              />
              <label className="flex items-center gap-2 text-sm text-zinc-600 dark:text-zinc-300">
                <input
                  type="checkbox"
                  checked={expose}
                  disabled={busy}
                  onChange={(e) => setExpose(e.target.checked)}
                  className="accent-sky-500"
                />
                <Globe size={14} />
                Expose externally at this address
              </label>
            </Field>
          ) : (
            <p className="text-sm text-zinc-400">
              No HTTP source — this integration runs as an internal workload with no
              address.
            </p>
          )}

          {envVars.length > 0 && (
            <Field
              label="Environment"
              hint="Fill each variable with a value or a cluster secret. Required ones are marked *."
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
            disabled={!canDeploy}
            className="inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white transition-colors hover:bg-sky-500 disabled:opacity-50"
          >
            <Rocket size={14} />
            Deploy
          </button>
        </footer>
      </div>
    </div>
  );
}
