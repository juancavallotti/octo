"use client";

import { useEffect, useState } from "react";
import { getEmbeddingStatus, type EmbeddingStatus } from "@/app/model/siteSettings";

/**
 * How search on this page ranks: by meaning, or by words.
 *
 * It is a line on the memory page rather than a panel under Admin, and the move
 * is the point. It was a settings page first — provider, model, API key, Save —
 * and it should not have been: the model cannot be changed once anything has been
 * embedded, since vectors carry no record of which model produced them and a
 * store holding two models' cannot be ranked coherently. Nothing read the value
 * until the orchestrator restarted, either. So it became chart values on the
 * embedding server, and what was left was a read-only report.
 *
 * A read-only report about search belongs where someone searches. Under Admin it
 * answered a question nobody was asking there; here it answers the one an
 * operator asks the moment a search returns something surprising.
 *
 * There is no progress bar. "How many are embedded" cannot be answered from an
 * index, so drawing one read both memory tables end to end on every load — see
 * PendingCount. What is outstanding is the useful half anyway: it is the reason
 * a search might not find something yet.
 */
export function SearchRanking() {
  const [status, setStatus] = useState<EmbeddingStatus | null>(null);

  // Failure is silent, and only here. This reports on search rather than being
  // search: an operator whose conversations are listed fine should not be handed
  // a red error about a status probe they did not ask for.
  useEffect(() => {
    getEmbeddingStatus().then(setStatus, () => setStatus(null));
  }, []);

  if (status === null) return null;

  return (
    <p className="mt-2 text-xs text-zinc-500 dark:text-zinc-400">
      {!status.configured ? (
        // Not a warning. Running without an embedding server is supported, and
        // colouring it as a fault sends someone looking for one that is not there.
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
