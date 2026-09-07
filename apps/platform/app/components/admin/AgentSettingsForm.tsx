"use client";

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useReducer,
} from "react";
import type { ReactNode } from "react";
import type { AgentStatus } from "@/app/model/agent";
import type { LlmSettings } from "@/app/model/siteSettings";

/**
 * One draft of everything on the agent page, and one Save.
 *
 * The page had four ways to commit a change: a Save under the provider, a Save
 * under the search key, an Apply beside the turn limit, and a checkbox that
 * committed the instant it was clicked. Four models of "is this written down
 * yet?" on one screen, and the checkbox's answer was different from the other
 * three.
 *
 * So the fields edit a draft and nothing is written until Save. What is dirty is
 * derived by comparing the draft with what was loaded, rather than tracked by
 * each field setting a flag — a flag has to be cleared correctly on every path,
 * including the one where you type a change and then type it back.
 *
 * The two secrets are the exception that shapes the type: an empty key means
 * "keep the stored one", so for those, dirty is "non-empty" rather than
 * "different". There is no draft value that means "the key I cannot see".
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

type Action =
  | { type: "loaded"; draft: AgentDraft }
  | { type: "set"; field: keyof AgentDraft; value: string | boolean }
  | { type: "committed"; draft: AgentDraft };

interface State {
  /** What is stored, as a draft, so dirty is one comparison. */
  base: AgentDraft | null;
  draft: AgentDraft | null;
}

function reducer(state: State, action: Action): State {
  switch (action.type) {
    // Loading replaces the baseline AND the draft: this is the first read, or a
    // re-read after a save, and in both cases what is on screen should become
    // what is stored.
    case "loaded":
    case "committed":
      return { base: action.draft, draft: action.draft };
    case "set":
      if (!state.draft) return state;
      return {
        ...state,
        draft: { ...state.draft, [action.field]: action.value },
      };
  }
}

/** Which parts of the draft differ from what is stored. */
export interface Dirty {
  llm: boolean;
  webSearch: boolean;
  /** Either of the two settings that live on the pods, so they save together. */
  deployment: boolean;
  any: boolean;
}

function dirtyOf(base: AgentDraft | null, draft: AgentDraft | null): Dirty {
  if (!base || !draft) {
    return { llm: false, webSearch: false, deployment: false, any: false };
  }
  const llm =
    draft.provider !== base.provider ||
    draft.model.trim() !== base.model.trim() ||
    // Non-empty rather than different: there is no draft value that means "the
    // key already stored", so anything typed here is a change by definition.
    draft.llmApiKey.length > 0;
  const webSearch = draft.webSearchApiKey.length > 0;
  const deployment =
    draft.maxIterations.trim() !== base.maxIterations.trim() ||
    draft.autoFix !== base.autoFix;
  return { llm, webSearch, deployment, any: llm || webSearch || deployment };
}

interface AgentFormValue {
  draft: AgentDraft | null;
  dirty: Dirty;
  set: (field: keyof AgentDraft, value: string | boolean) => void;
  loaded: (draft: AgentDraft) => void;
  committed: (draft: AgentDraft) => void;
}

const AgentFormContext = createContext<AgentFormValue | null>(null);

export function AgentSettingsForm({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(reducer, { base: null, draft: null });

  const set = useCallback(
    (field: keyof AgentDraft, value: string | boolean) =>
      dispatch({ type: "set", field, value }),
    [],
  );
  const loaded = useCallback(
    (draft: AgentDraft) => dispatch({ type: "loaded", draft }),
    [],
  );
  const committed = useCallback(
    (draft: AgentDraft) => dispatch({ type: "committed", draft }),
    [],
  );

  const value = useMemo(
    () => ({
      draft: state.draft,
      dirty: dirtyOf(state.base, state.draft),
      set,
      loaded,
      committed,
    }),
    [state, set, loaded, committed],
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

/** Build the draft a loaded page starts from. */
export function draftFrom(
  llm: LlmSettings | null,
  status: AgentStatus | null,
  fallbackProvider: string,
  fallbackModel: string,
): AgentDraft {
  return {
    provider: llm?.provider || fallbackProvider,
    model: llm?.model || fallbackModel,
    llmApiKey: "",
    webSearchApiKey: "",
    maxIterations: status?.maxIterations ? String(status.maxIterations) : "",
    autoFix: status?.autoFix ?? false,
  };
}
