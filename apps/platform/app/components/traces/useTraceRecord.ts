/**
 * One record's captured payloads, fetched when someone actually opens it.
 *
 * The trace itself is loaded without payloads — a waterfall needs every record's
 * shape and almost none of their contents, and the bodies are the whole weight
 * of a trace. This is the other half of that bargain: the one record a reader
 * clicks is fetched on its own.
 */

import { useEffect, useState } from "react";
import { getTraceRecord, type TraceRecord } from "@/app/model/traces";

export interface LoadedRecord {
  record: TraceRecord | null;
  loading: boolean;
  error: string | null;
}

/** Fetch one record of `traceId`. Passing a null id asks for nothing. */
export function useTraceRecord(traceId: string, id: string | null): LoadedRecord {
  const [record, setRecord] = useState<TraceRecord | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loaded, setLoaded] = useState<string | null>(null);

  useEffect(() => {
    if (id === null) return;
    let live = true;
    getTraceRecord(traceId, id).then(
      (fetched) => {
        if (!live) return;
        setRecord(fetched);
        setError(null);
        setLoaded(id);
      },
      (err: Error) => {
        if (!live) return;
        setError(err.message);
        setLoaded(id);
      },
    );
    return () => {
      live = false;
    };
  }, [traceId, id]);

  // What is in state belongs to `loaded`; anything else is still in flight, so
  // one record's payload can never be shown under another record's heading.
  const fresh = loaded === id;
  return {
    record: fresh ? record : null,
    loading: id !== null && !fresh,
    error: fresh ? error : null,
  };
}
