"use client";

import { useEditorState } from "../state/editorState";
import DndProvider from "./DndProvider";
import Sidebar from "./Sidebar";
import Canvas from "./Canvas";
import SettingsPanel from "./SettingsPanel";
import YamlPreview from "./YamlPreview";
import ResourcesView from "./resources/ResourcesView";

/**
 * The editor body between the header and the log panel. It reads `state.viewMode`
 * (set by the header's ViewModeToggle) to show either the visual editor — the
 * palette sidebar, flow canvas, and settings panel in one drag-and-drop session —
 * the read-only YAML preview, or the Resources tab (file browser + content
 * editor). The switch lives here rather than in EditorRoot because EditorRoot sits
 * above the state provider it would need to read.
 */
export default function EditorBody() {
  const { state } = useEditorState();

  if (state.viewMode === "yaml") return <YamlPreview />;
  if (state.viewMode === "resources") return <ResourcesView />;

  return (
    <DndProvider>
      <div className="flex flex-1 min-h-0">
        <Sidebar />
        <Canvas />
        <SettingsPanel />
      </div>
    </DndProvider>
  );
}
