/**
 * One app's trace list: a page of traces, and older pages on demand.
 *
 * Paging is keyset, not offset. The cursor the service returns is a
 * `(startedAt, traceId)` pair, and it is treated as opaque here — the client's
 * only job is to hand back exactly what it was given. That matters because
 * traces tie: a burst of requests starts many inside the same millisecond, and a
 * cursor on the timestamp alone would either skip the rows that tie across a page
 * boundary or serve them twice, in a list where a missing trace is precisely the
 * thing someone is looking for.
 */

import { useCallback, useEffect, useRef, useState } from "react";
import { listTraces, type TraceFilters, type TraceSummary } from "@/app/model/traces";

/** How many traces a page holds. */
export const PAGE_SIZE = 50;

export interface TraceList {
  traces: TraceSummary[];
  /** The cursor for the next older page, or null when the end has been reached. */
  older: string | null;
  loading: boolean;
  loadingMore: boolean;
  error: string | null;
  loadMore: () => void;
}

/**
 * Fetch traces matching `filters`. Passing null asks for nothing — which is what
 * an unselected app means, and is different from an app with no traces.
 */
export function useTraceList(filters: TraceFilters | null): TraceList {
  const [traces, setTraces] = useState<TraceSummary[]>([]);
  const [older, setOlder] = useState<string | null>(null);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // The query the current list was loaded for. Everything else is derived from
  // it, so a stale answer can never be mistaken for the current one.
  const wanted = filters === null ? null : JSON.stringify(filters);
  const [loaded, setLoaded] = useState<string | null>(null);

  useEffect(() => {
    if (filters === null || wanted === null) return;
    let live = true;
    listTraces({ ...filters, limit: PAGE_SIZE }).then(
      (page) => {
        if (!live) return;
        setTraces(page.items);
        setOlder(page.nextBefore);
        setError(null);
        setLoaded(wanted);
      },
      (err: Error) => {
        if (!live) return;
        setTraces([]);
        setOlder(null);
        setError(err.message);
        setLoaded(wanted);
      },
    );
    return () => {
      live = false;
    };
    // filters is compared by value through `wanted`; the object identity changes
    // on every render of the caller and is not what should drive a refetch.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [wanted]);

  // The query as of right now, reachable from a callback without re-creating it.
  const wantedNow = useRef(wanted);
  useEffect(() => {
    wantedNow.current = wanted;
  }, [wanted]);

  const loadMore = useCallback(() => {
    if (!older || !filters || loadingMore) return;
    // Which query this page belongs to. The main effect drops a stale answer by
    // going out of scope; a paged one has to be checked against what is current
    // when it lands, or clicking "Load older" and then switching app appends the
    // previous app's traces under the new app's heading — and leaves a cursor
    // that pages the wrong query from then on.
    const pagedFor = wanted;
    setLoadingMore(true);
    listTraces({ ...filters, before: older, limit: PAGE_SIZE })
      .then(
        (page) => {
          if (pagedFor !== wantedNow.current) return;
          // Appended without de-duplication: the composite cursor is exact, so a
          // repeat here would be the service disagreeing with itself rather than
          // something for this list to paper over.
          setTraces((prev) => [...prev, ...page.items]);
          setOlder(page.nextBefore);
          setError(null);
        },
        (err: Error) => {
          if (pagedFor === wantedNow.current) setError(err.message);
        },
      )
      .finally(() => setLoadingMore(false));
  }, [older, filters, loadingMore, wanted]);

  // What is in state belongs to whatever `loaded` names. When that is not what is
  // wanted — a different app, changed filters — it is withheld rather than shown
  // while the new query runs, so the previous app's traces never appear under the
  // current one's heading. Paging does not change `wanted`, so appending an older
  // page keeps the list visible.
  const fresh = loaded === wanted;
  return {
    traces: fresh ? traces : [],
    older: fresh ? older : null,
    loading: wanted !== null && !fresh,
    loadingMore,
    // Withheld on the same terms as the list: an error raised by the query that
    // was on screen a moment ago is not a fact about the one loading now, and
    // leaving it up would blame the new app for the old one's failure.
    error: fresh ? error : null,
    loadMore,
  };
}
