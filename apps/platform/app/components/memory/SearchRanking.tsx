"use client";

import { useEffect, useState } from "react";
import { getEmbeddingStatus, type EmbeddingStatus } from "@/app/model/siteSettings";

/**
 * How search on this page ranks: by meaning, or by words.
 *
 * A read-only report, since the model cannot be changed once anything has been
 * embedded: vectors carry no record of which model produced them, and a store
 * holding two models' cannot be ranked coherently. It is configured as chart
 * values on the embedding server.
 *
 * There is no progress bar. "How many are embedded" cannot be answered from an
 * index, so drawing one would read both memory tables end to end on every load.
 * What is outstanding is the useful half anyway: it is the reason a search might
 * not find something yet.
 */
export function SearchRanking() {
  const [status, setStatus] = useState<EmbeddingStatus | null>(null);

  // Failure is silent. This reports on search rather than being search: an
  // operator whose conversations are listed fine should not be handed a red error
  // about a status probe they did not ask for.
  useEffect(() => {
    getEmbeddingStatus().then(setStatus, () => setStatus(null));
  }, []);

  if (status === null) return null;

  return (
    <p className="mt-2 text-xs text-zinc-500 dark:text-zinc-400">
      {!status.configured ? (
        // Not a warning: running without an embedding server is supported.
        <>Search matches text. This installation has no embedding server.</>
      ) : !status.reachable ? (
        <span className="text-red-600 dark:text-red-400">
          The embedding server is deployed but not answering, so search is matching text.
          {status.detail ? ` ${status.detail}` : ""}
        </span>
      ) : (
        <>
          Search ranks by meaning, using <code className="font-mono">{status.model}</code>.
          {status.pending > 0 && (
            <>
              {" "}
              {status.pending.toLocaleString()} stored{" "}
              {status.pending === 1 ? "item is" : "items are"} still waiting for a vector, and
              matched by text until then.
            </>
          )}
        </>
      )}
    </p>
  );
}
