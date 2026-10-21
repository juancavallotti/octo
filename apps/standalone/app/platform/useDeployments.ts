"use client";

import { useCallback, useEffect, useState } from "react";
import { paasBridge } from "../desktop";
import type { DeploymentSummary, VersionSummary } from "./types";
import { cleanMessage } from "./useAction";

/**
 * The open folder's integration's deployments and versions, read again after every
 * operation finishes: a deploy, a scale or a rollout changes what they say.
 */
export function useDeployments(key: string, activityAt: number | undefined) {
  const [deployments, setDeployments] = useState<DeploymentSummary[] | null>(null);
  const [versions, setVersions] = useState<VersionSummary[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [nonce, setNonce] = useState(0);

  useEffect(() => {
    const paas = paasBridge();
    if (!paas) return;
    let cancelled = false;
    Promise.all([paas.deployments(), paas.linkedVersions()])
      .then(([d, v]) => {
        if (cancelled) return;
        setDeployments(d);
        setVersions([...v].sort((a, b) => b.createdAt.localeCompare(a.createdAt)));
        setError(null);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(cleanMessage(err instanceof Error ? err.message : String(err)));
      });
    return () => {
      cancelled = true;
    };
  }, [key, activityAt, nonce]);

  const reload = useCallback(() => setNonce((n) => n + 1), []);
  return { deployments, versions, error, reload };
}
