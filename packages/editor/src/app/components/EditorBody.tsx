"use client";

import { useEditorState } from "../state/editorState";
import DndProvider from "./DndProvider";
import DocumentBar from "./DocumentBar";
import Sidebar from "./Sidebar";
import Canvas from "./Canvas";
import SettingsPanel from "./SettingsPanel";
import YamlPreview from "./YamlPreview";
import ResourcesView from "./resources/ResourcesView";
import TestingView from "./testing/TestingView";

/**
 * The editor body between the header and the log panel. It reads `state.viewMode`
 * (set by the header's ViewModeToggle) to show either the visual editor — the
 * palette sidebar, flow canvas, and settings panel in one drag-and-drop session —
 * the read-only YAML preview, the Resources tab (file browser + content editor), or
 * the Testing tab (dolphin suites). The switch lives here rather than in EditorRoot
 * because EditorRoot sits above the state provider it would need to read.
 *
 * The document bar sits inside the middle column, between the palette and the
 * settings panel, rather than spanning the window above them: it belongs to the
 * document being drawn, and a full-width strip would cap two drawers it has nothing
 * to do with. The Resources and Testing tabs bring their own left panel and their
 * own controls, so they get no bar.
 */
export default function EditorBody({ files }: { files?: React.ReactNode }) {
  const { state } = useEditorState();

  // No drawers here, so the bar spans the view — it is still directly above the
  // thing it describes.
  if (state.viewMode === "yaml")
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <DocumentBar files={files} />
        <YamlPreview />
      </div>
    );
  if (state.viewMode === "resources") return <ResourcesView />;
  if (state.viewMode === "testing") return <TestingView />;

  return (
    <DndProvider>
      <div className="flex flex-1 min-h-0">
        <Sidebar />
        <div className="flex min-w-0 flex-1 flex-col">
          <DocumentBar files={files} />
          <Canvas />
        </div>
        <SettingsPanel />
      </div>
    </DndProvider>
  );
}
