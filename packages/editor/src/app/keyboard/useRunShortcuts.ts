"use client";

import { useEffect, useRef } from "react";
import { useRunCommands } from "../commands/useRunCommands";
import { useEditorState } from "../state/editorState";
import { isTypingTarget } from "./typing";

/**
 * Cmd/Ctrl+Enter runs. Cmd/Ctrl+. stops.
 *
 * Not the IDE convention: `Cmd+R` and `F5` are both *reload* in a browser tab, and this
 * editor is served in one.
 *
 * Enter runs *whatever the view is about*, the same rule the RUN control follows: the
 * integration on the canvas, the suites on the Testing tab. With Shift it runs one flow
 * once. Run never stops — `Cmd+.` is the one that means stop.
 */
export function useRunShortcuts(): void {
  const { state } = useEditorState();
  const commands = useRunCommands();

  // Held in a ref so the listener registers once but always sees the current closure,
  // as SaveContext's Cmd+S does. What each one does — and when it is a no-op — is
  // useRunCommands', shared with a host's Run menu.
  const act = useRef<(kind: "run" | "run-flow" | "stop") => void>(() => {});
  act.current = (kind) => {
    if (kind === "stop") commands.stop();
    // The Testing tab's RUN is the suites, with or without Shift.
    else if (kind === "run-flow" && state.viewMode !== "testing") commands.runFlow();
    else commands.run();
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

