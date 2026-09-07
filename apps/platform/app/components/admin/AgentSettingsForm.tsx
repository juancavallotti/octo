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
import { getAgentStatus, type AgentStatus } from "@/app/model/agent";
import {
  getLlmSettings,
  getWebSearchSettings,
  type LlmSettings,
  type WebSearchSettings,
} from "@/app/model/siteSettings";
import { providerById } from "./llmProviders";

/**
 * Everything the agent page reads and edits, in one place.
 *
 * The page used to be three components that each fetched their own settings,
 * held their own draft, and committed it with their own button — plus a checkbox
 * that committed the instant it was clicked. Four ideas of "is this written down
 * yet?" on one screen, and one of them different from the other three.
 *
 * The buttons were the visible half of that. The half underneath was that no
 * component could know what any other held, so nothing could decide what to save
 * or notice that two of the settings replace the same pods. Fixing the buttons
 * alone would have left that in place and hidden it better.
 *
 * So the state comes up here: one load, one draft, one notion of dirty. The
 * sections below are presentational — they render fields against this draft and
 * own nothing but their own prose.
 *
 * Two things deliberately stay out. Removing a stored key is immediate, because
 * it is destructive, it asks first, and a revocation deferred behind a Save that
 * is never pressed is a key someone believes is gone. And installing or removing
 * the agent is not a setting at all.
 */

/** The editable copy. Secrets are write-only: empty means keep what is stored. */
export interface AgentDraft {
  provider: string;
  model: string;
  llmApiKey: string;
  webSearchApiKey: string;
  /** Free text, because empty is a real value meaning "the definition decides". */
  maxIterations: string;
  autoFix: boolean;
}

/** What is stored, as read back from the three sources. */
interface Stored {
  llm: LlmSettings | null;
  webSearch: WebSearchSettings | null;
  status: AgentStatus | null;
}

type Action =
  | { type: "loading" }
  | { type: "loaded"; stored: Stored }
  | { type: "loadFailed"; error: string }
  | { type: "set"; field: keyof AgentDraft; value: string | boolean }
  | { type: "error"; error: string | null }
  | { type: "busy"; busy: boolean };

interface State {
  stored: Stored;
  /** What was loaded, as a draft, so dirty is one comparison. */
  base: AgentDraft | null;
  draft: AgentDraft | null;
  loading: boolean;
  loadFailed: boolean;
  busy: boolean;
  error: string | null;
}

const EMPTY: Stored = { llm: null, webSearch: null, status: null };

const initial: State = {
  stored: EMPTY,
  base: null,
  draft: null,
  loading: true,
  loadFailed: false,
  busy: false,
  error: null,
};

function draftOf(stored: Stored): AgentDraft {
  // Normalised through the provider list rather than taken as given: an
  // unconfigured site has no provider, and a stored one no longer offered would
  // leave the select showing something a save would not send.
  const provider = providerById(stored.llm?.provider ?? "").id;
  return {
    provider,
    model: stored.llm?.model || providerById(provider).defaultModel,
    llmApiKey: "",
    webSearchApiKey: "",
    maxIterations: stored.status?.maxIterations
      ? String(stored.status.maxIterations)
      : "",
    autoFix: stored.status?.autoFix ?? false,
  };
}

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case "loading":
      return { ...state, loading: true, loadFailed: false, error: null };
    // A load replaces the baseline AND the draft. This is the first read or a
    // re-read after a save, and in both cases what is on screen should become
    // what is stored — including the secrets, which go back to empty because
    // what is stored is not something this page can show back.
    case "loaded": {
      const draft = draftOf(action.stored);
      return {
        ...state,
        stored: action.stored,
        base: draft,
        draft,
        loading: false,
        loadFailed: false,
      };
    }
    case "loadFailed":
      return {
        ...state,
        loading: false,
        loadFailed: true,
        error: action.error,
      };
    case "set":
      if (!state.draft) return state;
      return {
        ...state,
        draft: { ...state.draft, [action.field]: action.value },
      };
    case "error":
      return { ...state, error: action.error };
    case "busy":
      return { ...state, busy: action.busy };
  }
}

/** Which parts of the draft differ from what is stored. */
export interface Dirty {
  llm: boolean;
  webSearch: boolean;
  /** Either setting that lives on the pods, so the two save together. */
  deployment: boolean;
  any: boolean;
}

function dirtyOf(base: AgentDraft | null, draft: AgentDraft | null): Dirty {
  const none = { llm: false, webSearch: false, deployment: false, any: false };
  if (!base || !draft) return none;
  const llm =
    draft.provider !== base.provider ||
    draft.model.trim() !== base.model.trim() ||
    // Non-empty rather than different: there is no draft value meaning "the key
    // already stored", so anything typed here is a change by definition.
    draft.llmApiKey.length > 0;
  const webSearch = draft.webSearchApiKey.length > 0;
  const deployment =
    draft.maxIterations.trim() !== base.maxIterations.trim() ||
    draft.autoFix !== base.autoFix;
  return { llm, webSearch, deployment, any: llm || webSearch || deployment };
}

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

export function AgentSettingsForm({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(reducer, initial);

  const reload = useCallback(async () => {
    dispatch({ type: "loading" });
    try {
      // Settled rather than all: the agent status needs a cluster and the site
      // settings do not, so one being unavailable must not blank the other two.
      const [llm, webSearch, status] = await Promise.all([
        getLlmSettings().catch(() => null),
        getWebSearchSettings().catch(() => null),
        getAgentStatus().catch(() => null),
      ]);
      if (llm === null && webSearch === null && status === null) {
        dispatch({ type: "loadFailed", error: "Could not read the settings." });
        return;
      }
      dispatch({ type: "loaded", stored: { llm, webSearch, status } });
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
