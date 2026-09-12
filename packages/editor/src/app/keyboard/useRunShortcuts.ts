"use client";

import { useEffect, useRef } from "react";
import { rootFlowIdOf } from "../model/document";
import { useTestSuites } from "../providers/TestSuiteProvider";
import { useFlowRun } from "../run/FlowRunContext";
import { useRun } from "../run/RunContext";
import { useSuiteRun } from "../run/SuiteRunContext";
import { useEditorState } from "../state/editorState";
import { planRunAll } from "../suite/runAll";
import { isTypingTarget } from "./typing";

/**
 * Cmd/Ctrl+Enter runs. Cmd/Ctrl+. stops.
 *
 * Not the IDE convention, deliberately. `Cmd+R` (Xcode, JetBrains) and `F5` (VS Code,
 * Visual Studio) are both *reload* in a browser tab, and this editor is served in one
 * — taking reload away from someone would be worse than not having the shortcut. The
 * convention this tool actually belongs to is the one where you execute the thing in
 * front of you: Postman, SQL clients, notebooks, and the CEL tab next door, which has
 * bound Cmd+Enter to "run this expression" since it was written.
 *
 * So Enter runs *whatever the view is about*, which is the same rule the RUN control
 * follows (see RunBar): the integration on the canvas, the suites on the Testing tab.
 * With Shift it runs one flow once — the inner loop, and more often what is wanted
 * than starting the whole integration.
 *
 * Run never stops. A key labelled "run" that halts a running integration because it
 * was already running is a surprise, and `Cmd+.` — Xcode's, and unclaimed by any
 * browser — is the one that means stop.
 */
export function useRunShortcuts(): void {
  const { state } = useEditorState();
  const run = useRun();
  const flowRun = useFlowRun();
  const suiteRun = useSuiteRun();
  const suites = useTestSuites();

  // Held in a ref so the listener registers once but always sees the current closure,
  // as SaveContext's Cmd+S does.
  const act = useRef<(kind: "run" | "run-flow" | "stop") => void>(() => {});
  act.current = (kind) => {
    if (kind === "stop") {
      if (run?.running && !run.busy) run.stop();
      return;
    }

    // The Testing tab's RUN is the suites, and nothing on it is about a live runner.
    if (state.viewMode === "testing") {
      if (!suiteRun || suiteRun.running || !suites?.loaded) return;
      const names = state.document.flows.map((f) => f.name).filter(Boolean);
      const plan = planRunAll(suites.all(), names);
      if (plan.targets.length > 0) void suiteRun.run(plan.targets, plan.skipped);
      return;
    }

    if (kind === "run-flow") {
      if (!flowRun || flowRun.busy) return;
      const flowId = currentFlowId(state);
      // No flow to run is not an error worth reporting: there is nothing on the
      // canvas, or nothing selected in a document with several.
      if (flowId) void flowRun.runFlow(flowId);
      return;
    }

    // Starting is the only thing Enter does. `validation.ok` is what the RUN button
    // disables itself on, and pressing a shortcut past it would start a run the
    // runner is about to refuse.
    if (!run || run.running || run.busy || !run.validation.ok) return;
    run.start();
  };

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey)) return;

      if (e.key === "Enter") {
        // Deliberately NOT guarded on the focused element, unlike every other shortcut
        // here and in the editor. Typing a URL into a block setting and pressing Run is
        // the commonest thing there is, and a Run key that quietly does nothing because
        // the caret is still in the field you just edited reads as a broken key, not as
        // a focus rule — which is exactly how it was reported.
        //
        // No plain field wants Cmd+Enter, so there is nothing to take away. The one
        // place that does want it is the CEL tab, which evaluates its expression with
        // it and stops the event itself (CelTester.onKeyDown) rather than relying on a
        // guard out here that cannot tell which fields care.
        e.preventDefault();
        act.current(e.shiftKey ? "run-flow" : "run");
        return;
      }
      if (e.key === ".") {
        // Still guarded: Cmd+. in a text field is the platform's own cancel.
        if (isTypingTarget(e.target)) return;
        e.preventDefault();
        act.current("stop");
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, []);
}

/**
 * Which flow Shift+Enter runs: the one the selection is in, else the one last
 * clicked, else the only one there is.
 *
 * A document with several flows and nothing selected has no answer, and guessing the
 * first would run something the user was not looking at.
 */
function currentFlowId(state: ReturnType<typeof useEditorState>["state"]): string | null {
  const doc = state.document;
  const selected = state.selectedBlockId ? rootFlowIdOf(doc, state.selectedBlockId) : null;
  if (selected) return selected;
  if (state.selectedSourceFlowId) return state.selectedSourceFlowId;
  if (state.activeFlowId) return state.activeFlowId;
  return doc.flows.length === 1 ? doc.flows[0].id : null;
}
