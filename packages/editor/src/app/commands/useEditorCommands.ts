"use client";

import { useEffect, useRef, useState } from "react";
import { useCanvasZoom } from "../canvas/ZoomContext";
import { isTypingTarget } from "../keyboard/typing";
import { useResourceStore } from "../providers/ResourceStoreProvider";
import { useTestSuites } from "../providers/TestSuiteProvider";
import { useConsoleCollapsed } from "../run/console";
import { useRunCommands } from "./useRunCommands";
import { useSave } from "../save/SaveContext";
import { EditorActionType, useEditorState } from "../state/editorState";
import { useLayout } from "../state/layout";
import { useNewItem, type NewItemKind } from "../state/useNewItem";
import type { FlowsView, ViewMode } from "../state/reducer";

/**
 * Everything the editor can be told to do from outside its own controls — a host's
 * native menu bar. One list, so a menu and the editor's own shortcuts cannot come to
 * mean different things by the same name.
 */
export type EditorCommand =
  | "undo"
  | "redo"
  | "save"
  /** Whatever the view runs: the integration, or on the Testing tab, the suites. */
  | "run"
  | "run-flow"
  | "stop"
  | `view:${ViewMode}`
  | `flows:${FlowsView}`
  | "toggle:components"
  | "toggle:settings"
  | "toggle:console"
  | "canvas:zoom-in"
  | "canvas:zoom-out"
  | "canvas:zoom-reset"
  | `new:${NewItemKind}`;

/**
 * What a host needs to show those commands truthfully: which are possible right now,
 * and which view and panels are showing. Plain data, so it can cross a process
 * boundary as is.
 */
export interface EditorCommandStatus {
  view: ViewMode;
  flowsView: FlowsView;
  hasResources: boolean;
  hasTests: boolean;
  /** Focus is in a text field, whose own undo history Undo and Redo then step. */
  editingText: boolean;
  canUndo: boolean;
  canRedo: boolean;
  canSave: boolean;
  /** What `run` runs here: the integration, or the suites on the Testing tab. */
  runs: "integration" | "tests";
  canRun: boolean;
  canRunFlow: boolean;
  canStop: boolean;
  /** The canvas is showing, so zoom means the canvas rather than the page. */
  canvas: boolean;
  /** Which panels are open; null when the host gave the editor no layout. */
  panels: { components: boolean; settings: boolean; console: boolean } | null;
}

export interface EditorCommands {
  run(command: EditorCommand): void;
  status: EditorCommandStatus;
}

/** Whether focus is in a text field, followed as it moves. */
function useEditingText(): boolean {
  const [editing, setEditing] = useState(false);
  useEffect(() => {
    const update = () => setEditing(isTypingTarget(document.activeElement));
    update();
    document.addEventListener("focusin", update);
    document.addEventListener("focusout", update);
    return () => {
      document.removeEventListener("focusin", update);
      document.removeEventListener("focusout", update);
    };
  }, []);
  return editing;
}

/**
 * The editor's commands and their status. Every command does what the editor's own
 * control for it does, and is a no-op when that control would be disabled, so a host
 * may forward one without checking first.
 */
export function useEditorCommands(): EditorCommands {
  const { state, dispatch, canUndo, canRedo } = useEditorState();
  const save = useSave();
  const runs = useRunCommands();
  const suites = useTestSuites();
  const hasResources = useResourceStore() !== null;
  const layout = useLayout();
  const consolePanel = useConsoleCollapsed();
  const zoom = useCanvasZoom();
  const newItem = useNewItem();
  const editingText = useEditingText();

  const status: EditorCommandStatus = {
    view: state.viewMode,
    flowsView: state.flowsView,
    hasResources,
    hasTests: suites !== null,
    editingText,
    canUndo,
    canRedo,
    canSave: !!save && !save.blocked && !save.busy,
    runs: runs.runs,
    canRun: runs.canRun,
    canRunFlow: runs.canRunFlow,
    canStop: runs.canStop,
    canvas: state.viewMode === "flows" && state.flowsView === "canvas",
    panels: layout
      ? { components: layout.sidebar, settings: layout.settings, console: !consolePanel.collapsed }
      : null,
  };

  // Read through a ref so a host can subscribe once and still act on the latest state.
  const act = useRef<(command: EditorCommand) => void>(() => {});
  act.current = (command) => {
    if (command.startsWith("new:")) {
      newItem(command.slice(4) as NewItemKind);
      return;
    }
    if (command.startsWith("view:")) {
      const view = command.slice(5) as ViewMode;
      if ((view === "resources" && !hasResources) || (view === "testing" && !suites)) return;
      dispatch({ type: EditorActionType.SET_VIEW_MODE, data: view });
      return;
    }
    if (command.startsWith("flows:")) {
      dispatch({ type: EditorActionType.SET_VIEW_MODE, data: "flows" });
      dispatch({ type: EditorActionType.SET_FLOWS_VIEW, data: command.slice(6) as FlowsView });
      return;
    }
    switch (command) {
      case "undo":
      case "redo":
        // In a text field the field's own history is the one meant, as it is for the
        // editor's Cmd+Z, which leaves typing alone.
        if (isTypingTarget(document.activeElement)) {
          document.execCommand(command);
          return;
        }
        if (command === "undo" ? canUndo : canRedo) {
          dispatch({ type: command === "undo" ? EditorActionType.UNDO : EditorActionType.REDO });
        }
        return;
      case "save":
        if (status.canSave) void save?.save();
        return;
      case "run":
        runs.run();
        return;
      case "run-flow":
        runs.runFlow();
        return;
      case "stop":
        runs.stop();
        return;
      case "toggle:components":
        layout?.toggleSidebar();
        return;
      case "toggle:settings":
        layout?.toggleSettings();
        return;
      case "toggle:console":
        consolePanel.toggle();
        return;
      case "canvas:zoom-in":
        zoom.zoomIn();
        return;
      case "canvas:zoom-out":
        zoom.zoomOut();
        return;
      case "canvas:zoom-reset":
        zoom.reset();
        return;
    }
  };

  const runCommand = useRef((command: EditorCommand) => act.current(command)).current;
  return { run: runCommand, status };
}
