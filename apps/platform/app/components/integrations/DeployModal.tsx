"use client";

import { useEffect, useState } from "react";
import { Rocket, X } from "lucide-react";
import type { EnvBindingInput, Snapshot } from "@/app/model/orchestrator";
import { suggestNextTag } from "@/app/model/tags";
import { useDeployOptions } from "./useDeployOptions";
import { useDeployEnv } from "./useDeployEnv";
import DeployFormFields from "./DeployFormFields";

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

/** What the modal submits: either an existing snapshot to deploy, or a new tag to
 * cut from the working copy first. The parent resolves the two into a deployment. */
export interface DeploySubmit {
  snapshotId?: string;
  newTag?: string;
  replicas: number;
  slug?: string;
  expose?: "external";
  env?: Record<string, EnvBindingInput>;
  tracing?: boolean;
  orchestratorApi?: boolean;
  observabilityApi?: boolean;
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
  const [tracing, setTracing] = useState(false);
  // Platform-access grants, both off by default: an integration that reads its own
  // installation is the exception, and the default should be the rule.
  const [orchestratorApi, setOrchestratorApi] = useState(false);
  const [observabilityApi, setObservabilityApi] = useState(false);
  // A tag reads its frozen definition, Current the live working copy. The
  // suggestion prefills the slug box on load, but not on the failure fallback —
  // see useDeployOptions.
  const opts = useDeployOptions(integrationId, activeSnapshot, (o) =>
    setSlug(o.suggestedSlug ?? ""),
  );
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
      ...(tracing ? { tracing: true } : {}),
      ...(orchestratorApi ? { orchestratorApi: true } : {}),
      ...(observabilityApi ? { observabilityApi: true } : {}),
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

        <DeployFormFields
          integrationId={integrationId}
          currentMode={currentMode}
          loading={opts === null}
          activeVersionLabel={activeSnapshot?.tag ?? ""}
          newTag={newTag}
          onNewTag={setNewTag}
          replicas={replicas}
          onReplicas={setReplicas}
          networked={networked}
          expose={expose}
          onExpose={setExpose}
          slug={slug}
          onSlug={setSlug}
          onSlugOk={setSlugOk}
          tracing={tracing}
          onTracing={setTracing}
          orchestratorApi={orchestratorApi}
          onOrchestratorApi={setOrchestratorApi}
          observabilityApi={observabilityApi}
          onObservabilityApi={setObservabilityApi}
          envVars={envVars}
          bindings={bindings}
          secretNames={secretNames}
          providedKeys={providedKeys}
          missingRequired={missingRequired}
          onBinding={setBinding}
          busy={busy}
          error={error}
        />

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
