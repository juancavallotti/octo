"use client";

import { useCallback, useEffect, useState } from "react";
import { Variable } from "lucide-react";
import { getDeployOptions, type DeployEnvVar } from "@/app/model/orchestrator";

/**
 * Read-only view of the environment variables the selected version's definition
 * declares: each variable's name, whether it is required, and its default (if
 * any). Only declarations are shown — never values — so nothing sensitive is
 * exposed here; values are set at deploy time in the Deploy modal. Scoped to a
 * tag's frozen definition when snapshotId is set, else the live working copy.
 */
export default function EnvSection({
  integrationId,
  snapshotId,
}: {
  integrationId: string;
  snapshotId?: string;
}) {
  const [envVars, setEnvVars] = useState<DeployEnvVar[]>([]);
  const [loaded, setLoaded] = useState(false);

  // A then-chain (not a synchronous setState in the effect) so switching versions
  // just refetches; the previous list stays until the new one resolves.
  const reload = useCallback(() => {
    getDeployOptions(integrationId, snapshotId ? { snapshotId } : {}).then(
      (o) => {
        setEnvVars(o.envVars ?? []);
        setLoaded(true);
      },
      () => {
        setEnvVars([]);
        setLoaded(true);
      },
    );
  }, [integrationId, snapshotId]);
  useEffect(() => {
    reload();
  }, [reload]);

  if (loaded && envVars.length === 0) {
    return (
      <p className="text-sm text-zinc-400">
        No environment variables declared.
      </p>
    );
  }

  return (
    <ul className="space-y-1.5">
      {envVars.map((ev) => (
        <li
          key={ev.name}
          className="flex items-center gap-2 rounded-md border border-black/10 px-2.5 py-1.5 text-sm dark:border-white/10"
        >
          <Variable size={14} className="shrink-0 text-zinc-400" />
          <span className="min-w-0 flex-1 truncate font-mono text-xs">
            {ev.name}
          </span>
          {ev.required ? (
            <span className="shrink-0 rounded bg-amber-500/10 px-1.5 py-0.5 text-xs font-medium text-amber-600 dark:text-amber-400">
              required
            </span>
          ) : ev.default ? (
            <span className="min-w-0 max-w-[50%] shrink-0 truncate font-mono text-xs text-zinc-400">
              default: {ev.default}
            </span>
          ) : (
            <span className="shrink-0 text-xs text-zinc-400">optional</span>
          )}
        </li>
      ))}
    </ul>
  );
}
