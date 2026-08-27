"use client";

import { useCallback, useEffect, useState } from "react";
import {
  getEmbeddingStatus,
  type EmbeddingStatus,
} from "@/app/model/siteSettings";
import { BackfillProgress } from "./BackfillProgress";

/**
 * What the embedding server is doing, if this installation has one.
 *
 * A panel and not a form, deliberately. This was a settings page — provider,
 * model, API key, Save — and it should not have been. The model cannot be
 * changed once anything has been embedded: vectors carry no record of which
 * model produced them, so a store holding two models' cannot be ranked
 * coherently, and keeping one embedding space at a time is what makes that
 * simplification safe rather than a corruption waiting for someone to change a
 * setting. A control whose correct use is "never" does not belong behind a Save
 * button, however loud the confirmation dialog.
 *
 * There was a second problem with it being a setting, and it is the one that
 * settled the question: nothing read the value until the orchestrator restarted.
 * Saving on this page and then wondering why nothing had changed is a worse
 * experience than not offering the control at all.
 *
 * So it is chart values on the embedding server — a small octo app that holds
 * the provider key, so that no integration pod has to — and this reports what
 * that server says about itself.
 */
export default function EmbeddingStatusPanel() {
  const [status, setStatus] = useState<EmbeddingStatus | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(
    () =>
      getEmbeddingStatus().then(setStatus, (e) =>
        setError((e as Error).message),
      ),
    [],
  );

  useEffect(() => {
    load();
  }, [load]);

  return (
    <section aria-labelledby="embedding-heading" className="mt-6">
      <h3 id="embedding-heading" className="text-sm font-semibold">
        Embeddings <span className="font-normal text-zinc-500">(optional)</span>
      </h3>
      <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
        Whether searching agent memory ranks by meaning or by words. Deploy the
        embedding server and it ranks by meaning; leave it out and search still
        works &mdash; it matches text. Configured where it is deployed rather
        than here, because changing the model discards every stored vector.
      </p>

      {error && <p className="mt-3 text-sm text-red-500">{error}</p>}

      <div className="mt-4 flex flex-col gap-2 rounded-lg border border-black/10 p-4 text-sm dark:border-white/10">
        {status === null ? (
          <p className="text-zinc-500">Loading&hellip;</p>
        ) : !status.configured ? (
          // Not a warning. Running without embeddings is supported, and colouring
          // it as a fault would send someone looking for one that is not there.
          <p className="text-zinc-500">
            No embedding server on this installation, so searching agent memory
            matches text. Set{" "}
            <code className="font-mono">embeddings.enabled</code> and{" "}
            <code className="font-mono">embeddings.apiKey</code> in the Helm
            values to turn it on.
          </p>
        ) : (
          <>
            <Row label="Status">
              {status.reachable ? (
                <span className="text-emerald-600 dark:text-emerald-400">
                  Ranking by meaning
                </span>
              ) : (
                <span className="text-red-600 dark:text-red-400">
                  Deployed, not answering
                </span>
              )}
            </Row>
            {status.model && (
              <Row label="Model">
                <code className="font-mono">{status.model}</code>
              </Row>
            )}
            {status.dimensions !== undefined && status.dimensions > 0 && (
              <Row label="Dimensions">{status.dimensions}</Row>
            )}
            {/* Verbatim, because it is what someone greps for. */}
            {status.detail && (
              <p className="font-mono text-xs break-words text-red-500">
                {status.detail}
              </p>
            )}
          </>
        )}
      </div>

      {status?.configured && (
        <BackfillProgress embedded={status.embedded} pending={status.pending} />
      )}
    </section>
  );
}

function Row({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-baseline justify-between gap-4">
      <span className="shrink-0 text-zinc-500">{label}</span>
      <span className="min-w-0 truncate text-right">{children}</span>
    </div>
  );
}
