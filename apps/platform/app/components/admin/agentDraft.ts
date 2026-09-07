/**
 * The agent page's draft, and the rules for reading it.
 *
 * Pure: no React, no fetching, no components. The reducer and the dirty
 * comparison are the whole of the page's logic worth reasoning about on their
 * own, and keeping them here means they can be read — and tested — without
 * standing up a provider around them.
 */

import type { AgentStatus } from "@/app/model/agent";
import type { LlmSettings, WebSearchSettings } from "@/app/model/siteSettings";
import { providerById } from "./llmProviders";

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
export interface Stored {
  llm: LlmSettings | null;
  webSearch: WebSearchSettings | null;
  status: AgentStatus | null;
}

export type Action =
  | { type: "loading" }
  | { type: "loaded"; stored: Stored; error: string | null }
  | { type: "loadFailed"; error: string }
  | { type: "set"; field: keyof AgentDraft; value: string | boolean }
  | { type: "error"; error: string | null }
  | { type: "busy"; busy: boolean };

export interface State {
  stored: Stored;
  /** What was loaded, as a draft, so dirty is one comparison. */
  base: AgentDraft | null;
  draft: AgentDraft | null;
  loading: boolean;
  loadFailed: boolean;
  busy: boolean;
  error: string | null;
}

export const EMPTY: Stored = { llm: null, webSearch: null, status: null };

export const initial: State = {
  stored: EMPTY,
  base: null,
  draft: null,
  loading: true,
  loadFailed: false,
  busy: false,
  error: null,
};

export function draftOf(stored: Stored): AgentDraft {
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

export function reducer(state: State, action: Action): State {
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
        // A source that failed while others succeeded still has something worth
        // saying — "orchestrator unreachable" is the answer to why the deployment
        // section is empty, and swallowing it leaves that section blank and mute.
        error: action.error,
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

export function dirtyOf(
  base: AgentDraft | null,
  draft: AgentDraft | null,
): Dirty {
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
