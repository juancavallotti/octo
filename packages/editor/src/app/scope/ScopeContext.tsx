"use client";

import { createContext, useContext, useMemo, type ReactNode } from "react";
import type { CelEntry } from "../cel/catalog";
import type { MemberProvider } from "../cel/complete";
import { useEditorMeta } from "../providers/EditorMetaProvider";
import { useTestSuites } from "../providers/TestSuiteProvider";
import { parseSuite } from "../suite/parse";
import {
  emptyEvidence,
  fromBlockMocks,
  fromSuite,
  fromTestInputs,
  mergeMessages,
} from "./evidence";
import { useEditorState } from "../state/editorState";
import { membersFor, rootsFor } from "./members";
import { buildIndex, scopeAt, type ScopeIndex } from "./walk";
import type { CelSite } from "./types";

/**
 * Giving every CEL field the scope of the place it is in.
 *
 * Two contexts, because they change at wildly different rates. The index is one walk
 * of the whole document, rebuilt when the document changes; the site is a cheap value
 * that each settings form re-provides freely.
 *
 * The site is declared by the component that OWNS the field, never read from
 * `state.selectedBlockId` inside the field itself: SettingsField is shared by block,
 * source, connector and testing forms, and a selection read there would hand a
 * connector's fields a block's scope.
 */

const IndexContext = createContext<ScopeIndex | null>(null);
const SiteContext = createContext<CelSite>({ kind: "document" });

export function ScopeIndexProvider({ children }: { children: ReactNode }) {
  const { state } = useEditorState();
  const meta = useEditorMeta();
  const suites = useTestSuites();
  const doc = state.document;

  // Everything the workspace already says about these messages. All of it authored
  // rather than captured, so none of it costs a run — and the suites and mocks are
  // committed alongside the flows, so it is there on a fresh checkout too.
  const evidence = useMemo(() => {
    const gathered = emptyEvidence();
    for (const flow of doc.flows) {
      if (!flow.name) continue;
      const inputs = meta?.inputs(flow.id) ?? [];
      if (inputs.length > 0) {
        gathered.root.set(flow.name, mergeMessages(gathered.root.get(flow.name), fromTestInputs(inputs)));
      }
      const suite = suites?.suiteFor(flow.name);
      if (suite) fromSuite(parseSuite(suite).suite, gathered);
    }
    fromBlockMocks(meta?.enabledMocks() ?? [], gathered);
    return gathered;
  }, [doc, meta, suites]);

  // Keyed on document identity: the reducer mints a new document for every edit and
  // reuses it for everything else, so this walks once per change rather than once
  // per render.
  const index = useMemo(() => buildIndex({ doc, evidence }), [doc, evidence]);

  return <IndexContext.Provider value={index}>{children}</IndexContext.Provider>;
}

/** Declare which CEL site the fields below belong to. */
export function CelScopeProvider({ site, children }: { site: CelSite; children: ReactNode }) {
  // Memoised on its parts, so the object literal every caller passes inline does not
  // re-provide — and re-render every CEL field below it — on each render.
  const key = `${site.kind}:${"blockId" in site ? site.blockId : ""}${"flowId" in site ? site.flowId : ""}`;
  const value = useMemo(() => site, [key]); // eslint-disable-line react-hooks/exhaustive-deps
  return <SiteContext.Provider value={value}>{children}</SiteContext.Provider>;
}

/**
 * The scope here, or null when no index is mounted.
 *
 * Deliberately reads nothing but the two contexts. A CEL field is rendered in places
 * that have no editor state at all — the standalone tester, a unit test — and a hook
 * that reached for the reducer would make the field throw in every one of them.
 */
function useScope() {
  const index = useContext(IndexContext);
  const site = useContext(SiteContext);
  return useMemo(() => (index ? scopeAt(index, site) : null), [index, site]);
}

/** Member completion for the CEL fields at this site, or undefined outside a provider. */
export function useCelMembers(): MemberProvider | undefined {
  const scope = useScope();
  return useMemo(() => (scope ? membersFor(scope) : undefined), [scope]);
}

/** The root names in scope here, or undefined outside a provider. */
export function useCelRoots(): CelEntry[] | undefined {
  const scope = useScope();
  return useMemo(() => (scope ? rootsFor(scope) : undefined), [scope]);
}
