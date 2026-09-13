"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useReducer,
} from "react";
import type { ReactNode } from "react";
import { getAgentStatus } from "@/app/model/agent";
import { getLlmSettings, getWebSearchSettings } from "@/app/model/siteSettings";
import {
  initial,
  reducer,
  dirtyOf,
  type AgentDraft,
  type Dirty,
  type Stored,
} from "./agentDraft";

/**
 * Everything the agent page reads and edits, in one place: one load, one draft,
 * one notion of dirty. The sections below are presentational — they render fields
 * against this draft and own nothing but their own prose.
 *
 * Two things stay out. Removing a stored key is immediate, because it is
 * destructive, it asks first, and a revocation deferred behind a Save that is
 * never pressed is a key someone believes is gone. And installing or removing the
 * agent is not a setting at all.
 */

interface AgentFormValue {
  stored: Stored;
  draft: AgentDraft | null;
  dirty: Dirty;
  loading: boolean;
  loadFailed: boolean;
  busy: boolean;
  error: string | null;
  set: (field: keyof AgentDraft, value: string | boolean) => void;
  setError: (error: string | null) => void;
  /** Re-read everything and reseed the draft. */
  reload: () => Promise<void>;
  /** Run a mutation, then reload — the shape every write here shares. */
  run: (fn: () => Promise<unknown>) => Promise<void>;
}

const AgentFormContext = createContext<AgentFormValue | null>(null);

export default function AgentSettingsForm({
  children,
}: {
  children: ReactNode;
}) {
  const [state, dispatch] = useReducer(reducer, initial);

  const reload = useCallback(async () => {
    dispatch({ type: "loading" });
    try {
      // Settled rather than all: the agent status needs a cluster and the site
      // settings do not, so one being unavailable must not blank the other two.
      // Failure is counted, not inferred from a null — a site with no LLM settings
      // and no search key legitimately resolves null for both.
      let failure: string | null = null;
      let failures = 0;
      const keep = <T,>(p: Promise<T>): Promise<T | null> =>
        p.catch((e: unknown) => {
          failures += 1;
          failure ??= (e as Error).message;
          return null;
        });
      const [llm, webSearch, status] = await Promise.all([
        keep(getLlmSettings()),
        keep(getWebSearchSettings()),
        keep(getAgentStatus()),
      ]);
      if (failures === 3) {
        dispatch({
          type: "loadFailed",
          error: failure ?? "Could not read the settings.",
        });
        return;
      }
      dispatch({
        type: "loaded",
        stored: { llm, webSearch, status },
        error: failure,
      });
    } catch (e) {
      dispatch({ type: "loadFailed", error: (e as Error).message });
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  const run = useCallback(
    async (fn: () => Promise<unknown>) => {
      dispatch({ type: "busy", busy: true });
      dispatch({ type: "error", error: null });
      try {
        await fn();
        await reload();
      } catch (e) {
        dispatch({ type: "error", error: (e as Error).message });
      } finally {
        dispatch({ type: "busy", busy: false });
      }
    },
    [reload],
  );

  const value = useMemo<AgentFormValue>(
    () => ({
      stored: state.stored,
      draft: state.draft,
      dirty: dirtyOf(state.base, state.draft),
      loading: state.loading,
      loadFailed: state.loadFailed,
      busy: state.busy,
      error: state.error,
      set: (field, value) => dispatch({ type: "set", field, value }),
      setError: (error) => dispatch({ type: "error", error }),
      reload,
      run,
    }),
    [state, reload, run],
  );

  return (
    <AgentFormContext.Provider value={value}>
      {children}
    </AgentFormContext.Provider>
  );
}

export function useAgentForm(): AgentFormValue {
  const value = useContext(AgentFormContext);
  if (!value) {
    throw new Error("useAgentForm must be used inside <AgentSettingsForm>");
  }
  return value;
}
