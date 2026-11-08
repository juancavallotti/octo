"use client";

import { useCallback, useEffect, useState } from "react";
import {
  createEnvironment,
  deleteEnvironment,
  listEnvironments,
  updateEnvironment,
  type Environment,
  type EnvironmentInput,
  type EnvironmentPatch,
} from "@/app/model/environments";

/**
 * The environments screen's data: the list, one busy flag, and the last error.
 * Every mutation re-reads the list afterwards rather than patching it locally,
 * because a create or delete changes what the orchestrator provisioned and the
 * list is the cheapest way to see what it actually did.
 */
export function useEnvironments() {
  const [environments, setEnvironments] = useState<Environment[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(
    () =>
      listEnvironments().then(setEnvironments, (e) =>
        setError((e as Error).message),
      ),
    [],
  );

  useEffect(() => {
    refresh();
  }, [refresh]);

  /** Run a mutation, then refresh; report a failure and resolve false. */
  const run = useCallback(
    async (fn: () => Promise<unknown>): Promise<boolean> => {
      setBusy(true);
      setError(null);
      try {
        await fn();
        await refresh();
        return true;
      } catch (e) {
        setError((e as Error).message);
        return false;
      } finally {
        setBusy(false);
      }
    },
    [refresh],
  );

  return {
    environments,
    busy,
    error,
    create: (input: EnvironmentInput) => run(() => createEnvironment(input)),
    update: (name: string, patch: EnvironmentPatch) =>
      run(() => updateEnvironment(name, patch)),
    remove: (name: string) => run(() => deleteEnvironment(name)),
  };
}
