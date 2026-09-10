"use client";

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import type { CelEvalResult } from "../run/transport";

/**
 * What the CEL tab is holding: the expression being tried, the sample vars and body
 * it runs against, and the log of what came back.
 *
 * Above the tab rather than inside it, for two reasons. A console tab unmounts when
 * you switch to Logs, and losing a half-written expression to a glance at the logs
 * is the opposite of a scratchpad. And the console's own clear button is in the
 * panel header, which cannot reach state that lives in the tab body.
 */

/** One evaluation, kept so the log can show it under the ones that came after. */
export interface CelRun {
  id: number;
  /** When it ran, already formatted — the log shows date and time. */
  at: string;
  expression: string;
  result: CelEvalResult | null;
  /** An input problem (bad JSON): never reached the runner. */
  error: string | null;
}

interface CelTesterValue {
  expression: string;
  setExpression(value: string): void;
  vars: string;
  setVars(value: string): void;
  body: string;
  setBody(value: string): void;
  log: CelRun[];
  record(run: Omit<CelRun, "id" | "at">): void;
  clear(): void;
}

const CelTesterContext = createContext<CelTesterValue | null>(null);

/** Kept short: this is a scratchpad, not a transcript. */
const MAX_RUNS = 50;

const stamp = () =>
  new Date().toLocaleString(undefined, {
    dateStyle: "short",
    timeStyle: "medium",
    // 24-hour whatever the locale prefers: this reads next to runner logs and the
    // Problems tab, which are all 24-hour, and an AM/PM suffix is width for nothing.
    hour12: false,
  });

export function CelTesterProvider({ children }: { children: ReactNode }) {
  const [expression, setExpression] = useState("");
  const [vars, setVars] = useState("");
  const [body, setBody] = useState("");
  const [log, setLog] = useState<CelRun[]>([]);

  const record = useCallback((run: Omit<CelRun, "id" | "at">) => {
    setLog((prev) =>
      [
        // Date.now() alone collides when two runs land in the same millisecond.
        { ...run, id: Date.now() + Math.random(), at: stamp() },
        ...prev,
      ].slice(0, MAX_RUNS),
    );
  }, []);

  const clear = useCallback(() => setLog([]), []);

  const value = useMemo<CelTesterValue>(
    () => ({
      expression,
      setExpression,
      vars,
      setVars,
      body,
      setBody,
      log,
      record,
      clear,
    }),
    [expression, vars, body, log, record, clear],
  );

  return (
    <CelTesterContext.Provider value={value}>
      {children}
    </CelTesterContext.Provider>
  );
}

/**
 * The CEL tab's state. Always mounted by EditorRoot, so a missing provider is a
 * wiring bug rather than a supported mode.
 */
export function useCelTester(): CelTesterValue {
  const value = useContext(CelTesterContext);
  if (!value) {
    throw new Error("useCelTester must be used within a CelTesterProvider");
  }
  return value;
}
