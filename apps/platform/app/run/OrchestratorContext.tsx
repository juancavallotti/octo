"use client";

import {
  createContext,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";
import { orchestratorAvailable } from "@/app/actions/orchestrator";

/**
 * Exposes whether the orchestrator integration features are available, by calling
 * the `orchestratorAvailable` server action once on mount. It is mounted high in
 * the tree (app/layout) so both the editor and the `/integrations` route share one
 * answer.
 *
 * Availability is opt-in: when `ORCHESTRATOR_URL` is unset the probe reports
 * false and the integration UI stays hidden, leaving the editor unchanged.
 */

interface OrchestratorContextValue {
  /** True once the orchestrator is configured and reachable. */
  available: boolean;
  /** True once the availability probe has resolved (so callers can avoid flashing UI). */
  ready: boolean;
}

const OrchestratorContext = createContext<OrchestratorContextValue | null>(null);

export function OrchestratorProvider({ children }: { children: ReactNode }) {
  const [available, setAvailable] = useState(false);
  const [ready, setReady] = useState(false);

  useEffect(() => {
    let cancelled = false;
    orchestratorAvailable()
      .then((ok) => {
        if (!cancelled) setAvailable(ok);
      })
      .catch(() => {})
      .finally(() => {
        if (!cancelled) setReady(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <OrchestratorContext.Provider value={{ available, ready }}>
      {children}
    </OrchestratorContext.Provider>
  );
}

export function useOrchestrator(): OrchestratorContextValue {
  const ctx = useContext(OrchestratorContext);
  if (!ctx) {
    throw new Error("useOrchestrator must be used within an OrchestratorProvider");
  }
  return ctx;
}
