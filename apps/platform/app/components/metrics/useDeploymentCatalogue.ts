"use client";

import { useEffect, useState } from "react";
import { readDeploymentCatalogue } from "@/app/model/stats";
import type { CataloguePage } from "./deploymentReads";
import { viewPreset, type View } from "./range";

/**
 * Every metric a deployment exposes, and the points behind all of them.
 *
 * The catalogue comes first and is what makes the rest bounded: it names the
 * metrics and says how many series each will resolve to, so the series requests
 * can be packed to a size the service will answer rather than guessed at.
 *
 * All of it is fetched at once — one round trip and a few hundred kilobytes for
 * a real deployment's fifty names — because the page exists to be scanned. A
 * chart that fills in when it scrolls into view is a chart nobody compares
 * against the one above it.
 */

export interface Catalogue extends CataloguePage {
  loading: boolean;
  error: string | null;
}

export function useDeploymentCatalogue(
  deploymentId: string,
  view: View,
  /** The grid's own clock: it polls slower than the overview chart. */
  now: number,
): Catalogue {
  const [state, setState] = useState<Omit<Catalogue, "loading">>({
    metrics: [],
    page: null,
    warnings: [],
    truncated: false,
    error: null,
    fromMs: now - viewPreset(view).askMs,
    toMs: now,
  });
  const [loaded, setLoaded] = useState<string | null>(null);

  const wanted = `${deploymentId} ${view} ${now}`;

  useEffect(() => {
    let cancelled = false;

    void (async () => {
      try {
        const catalogue = await readDeploymentCatalogue(deploymentId, view, now);
        if (cancelled) return;
        setState({ ...catalogue, error: null });
      } catch (err) {
        if (cancelled) return;
        // The last good catalogue stays on screen; only the error is new.
        setState((prev) => ({
          ...prev,
          error: err instanceof Error ? err.message : String(err),
        }));
      } finally {
        if (!cancelled) setLoaded(wanted);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [deploymentId, view, now, wanted]);

  return { ...state, loading: loaded !== wanted };
}
