"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Globe, Rocket } from "lucide-react";
import {
  createDeployment,
  deleteDeployment,
  listDeployments,
  type Deployment,
} from "@/app/model/orchestrator";
import DeploymentRow from "./DeploymentRow";

/**
 * Deployments for one integration: a one-click Deploy plus a list of live
 * deployments with their status and an Undeploy action. The orchestrator pushes
 * status changes over SSE (it watches the cluster), so the list updates live; if
 * the stream is unavailable we fall back to gentle polling. Mutations refresh
 * immediately, mirroring IntegrationsManager.
 */

// Polling cadence used only as a fallback when the SSE stream is unavailable.
const FALLBACK_POLL_MS = 5000;

export default function DeploymentsSection({
  integrationId,
}: {
  integrationId: string;
}) {
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Deploy options.
  const [replicas, setReplicas] = useState(1);
  const [expose, setExpose] = useState(false);
  const [subdomain, setSubdomain] = useState("");

  // A then-chain (not an async body) so the effect's call doesn't setState
  // synchronously — same shape as IntegrationsManager's refresh.
  const refresh = useCallback(
    () =>
      listDeployments(integrationId).then(
        (items) => {
          setDeployments(items);
          setError(null);
        },
        (e) => setError((e as Error).message),
      ),
    [integrationId],
  );

  // Live updates over SSE, with a polling fallback that engages only while the
  // stream is erroring (e.g. orchestrator without informer support).
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  useEffect(() => {
    const stopPoll = () => {
      if (pollRef.current) {
        clearInterval(pollRef.current);
        pollRef.current = null;
      }
    };
    refresh(); // first paint, independent of the stream connecting
    const es = new EventSource(
      `/api/integrations/${encodeURIComponent(integrationId)}/deployments/events`,
    );
    es.onmessage = (ev) => {
      try {
        setDeployments(JSON.parse(ev.data) as Deployment[]);
        setError(null);
      } catch {
        /* ignore a malformed frame; the next one replaces it */
      }
    };
    es.onopen = stopPoll; // stream healthy → no need to poll
    es.onerror = () => {
      // Stream dropped or unavailable; keep the list fresh until it recovers.
      if (!pollRef.current) {
        pollRef.current = setInterval(refresh, FALLBACK_POLL_MS);
      }
    };
    return () => {
      es.close();
      stopPoll();
    };
  }, [integrationId, refresh]);

  /** Run a mutation, then refresh; surface failures inline. */
  const run = useCallback(
    async (fn: () => Promise<unknown>) => {
      setBusy(true);
      setError(null);
      try {
        await fn();
        await refresh();
      } catch (e) {
        setError((e as Error).message);
      } finally {
        setBusy(false);
      }
    },
    [refresh],
  );

  const deploy = () =>
    run(() =>
      createDeployment(integrationId, {
        replicas,
        ...(expose
          ? { expose: "external", subdomain: subdomain.trim() || undefined }
          : {}),
      }),
    );

  const undeploy = (d: Deployment) => {
    if (!confirm(`Undeploy "${d.name}" (${d.id.slice(0, 8)})?`)) return;
    run(() => deleteDeployment(d.id));
  };

  return (
    <>
      <div className="mb-2 flex flex-wrap items-end gap-3">
        <label className="flex flex-col text-xs text-zinc-500">
          Replicas
          <input
            type="number"
            min={1}
            value={replicas}
            onChange={(e) =>
              setReplicas(Math.max(1, Number(e.target.value) || 1))
            }
            disabled={busy}
            className="mt-0.5 w-16 rounded-md border border-zinc-300 bg-transparent px-2 py-1 text-sm dark:border-zinc-700"
          />
        </label>

        <label className="flex items-center gap-1.5 text-sm text-zinc-600 dark:text-zinc-300">
          <input
            type="checkbox"
            checked={expose}
            onChange={(e) => setExpose(e.target.checked)}
            disabled={busy}
          />
          <Globe size={14} />
          Expose externally
        </label>

        {expose && (
          <label className="flex flex-col text-xs text-zinc-500">
            Subdomain
            <input
              type="text"
              value={subdomain}
              onChange={(e) => setSubdomain(e.target.value)}
              placeholder="defaults to name"
              disabled={busy}
              className="mt-0.5 w-40 rounded-md border border-zinc-300 bg-transparent px-2 py-1 text-sm dark:border-zinc-700"
            />
          </label>
        )}

        <button
          type="button"
          onClick={deploy}
          disabled={busy}
          className="ml-auto inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white hover:bg-sky-500 disabled:opacity-50"
        >
          <Rocket size={14} />
          Deploy
        </button>
      </div>

      {error && <p className="mb-2 text-sm text-red-500">{error}</p>}

      {deployments.length === 0 ? (
        <p className="text-sm text-zinc-400">Not deployed.</p>
      ) : (
        <ul className="space-y-1.5">
          {deployments.map((d) => (
            <DeploymentRow
              key={d.id}
              deployment={d}
              busy={busy}
              onUndeploy={undeploy}
            />
          ))}
        </ul>
      )}
    </>
  );
}
