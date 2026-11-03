"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { rolloutDeployment } from "@/app/model/orchestrator";
import {
  listUpgrades,
  startTestRun,
  type NamedDeploymentUpgrade,
  type NamedUpgradesView,
} from "@/app/model/upgrades";

/** How often the view is read again while a test run is going. */
const ACTIVE_POLL_MS = 3000;
/** How often it is read again otherwise. */
const IDLE_POLL_MS = 15000;

/** The upgrades page's state and the actions it offers on each deployment. */
export interface UpgradesController {
  view: NamedUpgradesView | null;
  error: string | null;
  /** Deployments a rollout would move onto a newer runtime. */
  behind: NamedDeploymentUpgrade[];
  /** Deployments whose running version has no test suites. */
  untested: NamedDeploymentUpgrade[];
  /** Deployment ids with an action in flight. */
  pending: ReadonlySet<string>;
  refresh: () => Promise<void>;
  test: (d: NamedDeploymentUpgrade, rolloutOnPass: boolean) => Promise<void>;
  /** Roll a deployment onto the version it runs, which moves it to this runtime. */
  upgrade: (d: NamedDeploymentUpgrade) => Promise<void>;
  /** Test every behind deployment that has suites; with `rolloutOnPass`, each one
   * whose suites pass is upgraded. */
  testAll: (rolloutOnPass: boolean) => Promise<void>;
}

export function useUpgrades(): UpgradesController {
  const [view, setView] = useState<NamedUpgradesView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState<ReadonlySet<string>>(new Set());

  const refresh = useCallback(
    () =>
      listUpgrades().then(
        (v) => {
          setView(v);
          setError(null);
        },
        (e) => setError((e as Error).message),
      ),
    [],
  );

  const running = useMemo(
    () =>
      view?.deployments.some((d) => d.latestRun?.status === "running") ?? false,
    [view],
  );

  useEffect(() => {
    void refresh();
    const id = setInterval(refresh, running ? ACTIVE_POLL_MS : IDLE_POLL_MS);
    return () => clearInterval(id);
  }, [refresh, running]);

  // One action per deployment at a time; the id is held until the view has been
  // read back, so the row shows what the action did rather than flicker.
  const act = useCallback(
    async (id: string, fn: () => Promise<unknown>) => {
      setPending((p) => new Set(p).add(id));
      try {
        await fn();
        setError(null);
      } catch (e) {
        setError((e as Error).message);
      } finally {
        await refresh();
        setPending((p) => {
          const next = new Set(p);
          next.delete(id);
          return next;
        });
      }
    },
    [refresh],
  );

  const test = useCallback(
    (d: NamedDeploymentUpgrade, rolloutOnPass: boolean) =>
      act(d.deploymentId, () =>
        startTestRun(d.deploymentId, { rolloutOnPass }),
      ),
    [act],
  );

  const upgrade = useCallback(
    (d: NamedDeploymentUpgrade) =>
      act(d.deploymentId, async () => {
        if (!d.snapshotId)
          throw new Error(
            `${d.integrationName} runs no version tag to roll out`,
          );
        await rolloutDeployment(d.deploymentId, d.snapshotId);
      }),
    [act],
  );

  // By name, so a row stays where it was across polls.
  const sorted = useMemo(
    () =>
      [...(view?.deployments ?? [])].sort((a, b) =>
        a.integrationName.localeCompare(b.integrationName),
      ),
    [view],
  );
  const behind = useMemo(() => sorted.filter((d) => d.upgradeTo), [sorted]);
  const untested = useMemo(() => sorted.filter((d) => !d.hasSuites), [sorted]);

  const testAll = useCallback(
    async (rolloutOnPass: boolean) => {
      for (const d of behind) {
        if (d.hasSuites && d.latestRun?.status !== "running")
          await test(d, rolloutOnPass);
      }
    },
    [behind, test],
  );

  return {
    view,
    error,
    behind,
    untested,
    pending,
    refresh,
    test,
    upgrade,
    testAll,
  };
}
