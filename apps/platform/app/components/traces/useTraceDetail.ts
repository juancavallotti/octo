/**
 * One trace, fetched once and rebuilt into a tree.
 *
 * The fetch asks for no payloads. A waterfall needs every record's shape and
 * almost none of their contents, and the captured bodies are the whole weight of
 * a trace — the record inspector fetches the one a reader actually opens.
 *
 * The tree is built here rather than in the component so that zooming, panning
 * and collapsing never rebuild it: those are arithmetic over a value that has
 * already been computed.
 */

import { useEffect, useMemo, useState } from "react";
import { getTrace, type TraceDetail } from "@/app/model/traces";
import { buildWaterfall } from "./buildWaterfall";
import type { Waterfall } from "./types";

export interface TraceDetailState {
  detail: TraceDetail | null;
  waterfall: Waterfall | null;
  loading: boolean;
  error: string | null;
}

export function useTraceDetail(traceId: string | null): TraceDetailState {
  const [detail, setDetail] = useState<TraceDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loaded, setLoaded] = useState<string | null>(null);

  useEffect(() => {
    if (traceId === null) return;
    let live = true;
    getTrace(traceId, { bodies: false }).then(
      (fetched) => {
        if (!live) return;
        setDetail(fetched);
        setError(null);
        setLoaded(traceId);
      },
      (err: Error) => {
        if (!live) return;
        setError(err.message);
        setLoaded(traceId);
      },
    );
    return () => {
      live = false;
    };
  }, [traceId]);

  // What is in state belongs to `loaded`; when that is not the trace now
  // selected it is withheld, so one trace's waterfall never appears under
  // another's heading while the new one is in flight.
  const fresh = loaded === traceId;
  const current = fresh ? detail : null;

  const waterfall = useMemo(
    () =>
      current === null
        ? null
        : buildWaterfall(current.items, {
            // The stored rollup's bounds, so the duration the list showed and the
            // width of this chart are the same number by construction.
            startedAt: current.summary.startedAt,
            endedAt: current.summary.endedAt,
          }),
    [current],
  );

  return {
    detail: current,
    waterfall,
    loading: traceId !== null && !fresh,
    error: fresh ? error : null,
  };
}
