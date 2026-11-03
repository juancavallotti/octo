"use client";

import { useEffect, useState } from "react";
import { listRuntimes, type Runtimes } from "@/app/model/orchestrator";

/**
 * The runtime releases a deployment can be pinned to, loaded once. Null until
 * they arrive; an install that cannot say lists none, and only "latest" is
 * offered.
 */
export function useRuntimes(): Runtimes | null {
  const [runtimes, setRuntimes] = useState<Runtimes | null>(null);
  useEffect(() => {
    let live = true;
    listRuntimes().then(
      (r) => live && setRuntimes(r),
      () => live && setRuntimes({ versions: [] }),
    );
    return () => {
      live = false;
    };
  }, []);
  return runtimes;
}
