"use client";

import { useCallback, useEffect, useState } from "react";
import { paasBridge } from "../desktop";
import type { IntegrationSummary } from "./types";
import { cleanMessage } from "./useAction";

interface Loaded {
  key: string;
  items: IntegrationSummary[] | null;
  error: string | null;
}

/**
 * One installation's integrations, read again when it changes or when asked. What was
 * read is kept with the request it answered, so another installation's list is never
 * shown under this one while its own loads.
 */
export function useIntegrations(profile: string | null, signedIn: boolean) {
  const [loaded, setLoaded] = useState<Loaded | null>(null);
  const [nonce, setNonce] = useState(0);
  const key = `${profile}#${nonce}`;

  useEffect(() => {
    const paas = paasBridge();
    if (!paas || !profile || !signedIn) return;
    let cancelled = false;
    paas
      .integrations(profile)
      .then((items) => {
        if (!cancelled) setLoaded({ key, items, error: null });
      })
      .catch((err: unknown) => {
        if (!cancelled) setLoaded({ key, items: null, error: cleanMessage(err instanceof Error ? err.message : String(err)) });
      });
    return () => {
      cancelled = true;
    };
  }, [profile, signedIn, key]);

  const reload = useCallback(() => setNonce((n) => n + 1), []);
  const current = loaded?.key === key ? loaded : null;
  return { items: current?.items ?? null, error: current?.error ?? null, reload };
}
