"use client";

import { useEffect, useRef, useState } from "react";
import { Rocket } from "lucide-react";
import { useSave } from "@octo/editor";
import {
  createDeployment,
  createSnapshot,
  getIntegration,
  listDeployments,
  listSnapshots,
  rolloutDeployment,
  type Snapshot,
} from "@/app/model/orchestrator";
import { DEFAULT_TAG, suggestNextTag } from "@/app/model/tags";
import DeployModal, { type DeploySubmit } from "./integrations/DeployModal";

/**
 * Editor-header control that ships the current integration in one step: it saves,
 * tags a new version (the field prefills the next revision, still editable), then
 * rolls the integration's live deployment(s) over to that tag. When nothing is
 * deployed yet there's no rollout target, so it opens the deploy modal instead,
 * defaulted to the version it just created.
 *
 * Mirrors {@link TagButton}: renders nothing without a filesystem capability, is
 * disabled on an empty document, and reads the authoritative id from
 * `getIntegrationId` (a ref the host updates on save) after saving.
 */
export default function DeployButton({
  getIntegrationId,
}: {
  getIntegrationId: () => string | null;
}) {
  const save = useSave();
  const [open, setOpen] = useState(false);
  const [tag, setTag] = useState(DEFAULT_TAG);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Set once we've tagged but found nothing to roll out: opens the deploy modal
  // for a first-time deploy. Carries the id and display name it needs.
  const [firstDeploy, setFirstDeploy] = useState<{
    id: string;
    name: string;
    snapshot: Snapshot;
  } | null>(null);
  const ref = useRef<HTMLDivElement>(null);

  // Dismiss the popup on outside click / Escape (matches TagButton).
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  // No filesystem capability => nothing to deploy (mirrors how Save hides).
  if (!save) return null;

  // Open the popup, prefilling the tag with the suggested next revision (see
  // TagButton). The user can still edit it before deploying.
  const openPopup = () => {
    setError(null);
    setTag(DEFAULT_TAG);
    setOpen(true);
    const id = getIntegrationId();
    if (id) {
      listSnapshots(id).then(
        (snaps) => setTag(suggestNextTag(snaps.map((s) => s.tag))),
        () => {},
      );
    }
  };

  const deploy = async () => {
    const name = tag.trim();
    if (!name || busy) return;
    setBusy(true);
    setError(null);
    try {
      // Save first so the snapshot captures the on-screen definition; the first
      // save mints the id we read via the ref.
      await save.save();
      const id = getIntegrationId();
      if (!id) {
        setError("Save the integration before deploying.");
        return;
      }
      const snapshot = await createSnapshot(id, name);
      const deployments = await listDeployments(id);
      if (deployments.length > 0) {
        // Repoint every live deployment to the freshly tagged version.
        for (const d of deployments) {
          await rolloutDeployment(d.id, snapshot.id);
        }
        setOpen(false);
        return;
      }
      // Nothing deployed yet: hand off to the deploy modal to ship the tag we just
      // created (fixed-version mode — no re-tagging in the modal).
      const integration = await getIntegration(id);
      setFirstDeploy({ id, name: integration.name, snapshot });
      setOpen(false);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const submitFirstDeploy = async (input: DeploySubmit) => {
    if (!firstDeploy || !input.snapshotId) return;
    setBusy(true);
    setError(null);
    try {
      await createDeployment(firstDeploy.id, {
        snapshotId: input.snapshotId,
        replicas: input.replicas,
        ...(input.slug ? { slug: input.slug } : {}),
        ...(input.expose ? { expose: input.expose } : {}),
        ...(input.env ? { env: input.env } : {}),
      });
      setFirstDeploy(null);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => (open ? setOpen(false) : openPopup())}
        disabled={save.empty}
        title={save.empty ? "Nothing to deploy yet" : "Deploy this integration"}
        aria-haspopup="dialog"
        aria-expanded={open}
        className="inline-flex items-center gap-1.5 rounded-md bg-emerald-600 px-2.5 py-1 text-sm font-medium text-white transition-colors hover:bg-emerald-500 disabled:opacity-50"
      >
        <Rocket size={14} />
        Deploy
      </button>

      {open && (
        <div
          role="dialog"
          aria-label="Deploy this version"
          className="absolute right-0 top-full z-30 mt-2 w-64 rounded-xl border border-black/10 bg-white p-3 shadow-lg dark:border-white/10 dark:bg-zinc-900"
        >
          <label className="mb-1 block text-xs font-medium text-zinc-500">
            Version tag
          </label>
          <input
            autoFocus
            value={tag}
            disabled={busy}
            placeholder="e.g. v1.0.0"
            onChange={(e) => setTag(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") deploy();
            }}
            className="w-full rounded-md border border-black/10 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:border-white/15 dark:focus:border-white/30"
          />
          <p className="mt-1.5 text-xs text-zinc-500">
            Tags this version and rolls out your live deployment.
          </p>
          {error && <p className="mt-1.5 text-xs text-red-500">{error}</p>}
          <div className="mt-2 flex justify-end gap-2">
            <button
              type="button"
              onClick={() => setOpen(false)}
              disabled={busy}
              className="rounded-md px-2.5 py-1 text-sm text-zinc-600 hover:bg-black/[0.06] disabled:opacity-50 dark:text-zinc-300 dark:hover:bg-white/[0.08]"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={deploy}
              disabled={busy || !tag.trim()}
              className="inline-flex items-center gap-1.5 rounded-md bg-emerald-600 px-2.5 py-1 text-sm font-medium text-white hover:bg-emerald-500 disabled:opacity-50"
            >
              <Rocket size={13} />
              {busy ? "Deploying…" : "Deploy"}
            </button>
          </div>
        </div>
      )}

      {firstDeploy && (
        <DeployModal
          integrationId={firstDeploy.id}
          integrationName={firstDeploy.name}
          activeSnapshot={firstDeploy.snapshot}
          snapshots={[firstDeploy.snapshot]}
          busy={busy}
          error={error}
          onSubmit={submitFirstDeploy}
          onClose={() => {
            if (!busy) {
              setFirstDeploy(null);
              setError(null);
            }
          }}
        />
      )}
    </div>
  );
}
