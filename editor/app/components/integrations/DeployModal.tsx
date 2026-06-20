"use client";

import { useEffect, useState } from "react";
import { Globe, Rocket, X } from "lucide-react";
import {
  getDeployOptions,
  type DeploymentInput,
  type DeployOptions,
} from "@/app/model/orchestrator";
import SlugField from "./SlugField";

/**
 * Modal that collects per-deploy options and creates a deployment. It holds scale
 * (replicas) and, for an integration with an HTTP source, the deployment's address
 * slug (validated live) plus optional external exposure. Non-networked integrations
 * (timers, scheduled jobs) get neither — they run as a bare workload. Laid out as
 * labelled sections so future controls (env vars, secrets) drop in cleanly.
 *
 * The parent owns the deploy call (so it can refresh its list and surface errors);
 * this component owns the form state and closes itself on a successful submit.
 */

const INPUT =
  "rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/** A labelled group of fields; the unit future deploy options plug into. */
function Field({
  label,
  children,
  hint,
}: {
  label: string;
  children: React.ReactNode;
  hint?: string;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-xs font-semibold uppercase tracking-wide text-zinc-400">
        {label}
      </span>
      {children}
      {hint && <span className="text-xs text-zinc-400">{hint}</span>}
    </div>
  );
}

export default function DeployModal({
  integrationId,
  integrationName,
  busy,
  error,
  onSubmit,
  onClose,
}: {
  integrationId: string;
  integrationName: string;
  busy: boolean;
  error: string | null;
  onSubmit: (input: DeploymentInput) => void;
  onClose: () => void;
}) {
  const [replicas, setReplicas] = useState(1);
  const [expose, setExpose] = useState(false);
  const [slug, setSlug] = useState("");
  const [slugOk, setSlugOk] = useState(false);
  const [opts, setOpts] = useState<DeployOptions | null>(null);

  // Load deploy options once: whether the integration is networked, and a free
  // slug to prefill. On failure assume non-networked (deploy still works).
  useEffect(() => {
    let active = true;
    getDeployOptions(integrationId).then(
      (o) => {
        if (!active) return;
        setOpts(o);
        setSlug(o.suggestedSlug ?? "");
      },
      () => active && setOpts({ networked: false, slugValid: false, slugAvailable: false }),
    );
    return () => {
      active = false;
    };
  }, [integrationId]);

  // Close on Escape, mirroring the editor's other overlays.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !busy) onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [busy, onClose]);

  const networked = opts?.networked ?? false;
  const canDeploy = !busy && opts !== null && (!networked || slugOk);

  const submit = () => {
    if (!canDeploy) return;
    onSubmit({
      replicas,
      ...(networked ? { slug: slug.trim() } : {}),
      ...(networked && expose ? { expose: "external" } : {}),
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
