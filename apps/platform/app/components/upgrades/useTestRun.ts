"use client";

import { useEffect, useState } from "react";
import { getTestRun } from "@/app/model/upgrades";
import { isFinished, type TestRun } from "@/app/model/upgradeTypes";

/** How often a running test run is read again. */
const POLL_MS = 2000;

/**
 * Follows a test run until it finishes: the run as last read, and the error of
 * the last read when it failed. Calls `onFinished` once, with the finished run.
 */
export function useTestRun(
  initial: TestRun,
  onFinished?: (run: TestRun) => void,
): { run: TestRun; error: string | null } {
  const [run, setRun] = useState(initial);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (isFinished(run)) {
      onFinished?.(run);
      return;
    }
    const timer = setTimeout(() => {
      getTestRun(run.deploymentId, run.id).then(
        (next) => {
          setError(null);
          setRun(next);
        },
        (e) => setError((e as Error).message),
      );
    }, POLL_MS);
    return () => clearTimeout(timer);
    // onFinished is deliberately not a dependency: a caller's new closure must not
    // re-announce a run that already finished.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [run, error]);

  return { run, error };
}
