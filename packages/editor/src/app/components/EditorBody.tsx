"use client";

import { useEditorState } from "../state/editorState";
import { useLayout } from "../state/layout";
import { useHistoryShortcuts } from "../keyboard/useHistoryShortcuts";
import ComponentPalette from "./ComponentPalette";
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
 *
 * Whether the palette and the settings panel are showing is the header's layout
 * toggles' business (see LayoutToggles); this is where that choice is spent.
 */
export default function EditorBody({ files }: { files?: React.ReactNode }) {
  const { state } = useEditorState();
  const layout = useLayout();
  // Above the view switch, because undo is about the document and the document is
  // edited from the settings panel and the resources view too, not just the canvas.
  useHistoryShortcuts();

  const body = () => {
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
          {/* Hidden, not collapsed to a rail: the header toggles say where they are
              and bring them back, so a stub would only spend canvas. */}
          {layout?.sidebar !== false && <Sidebar />}
          <div className="flex min-w-0 flex-1 flex-col">
            <DocumentBar files={files} />
            <Canvas />
          </div>
          {layout?.settings !== false && <SettingsPanel />}
        </div>
      </DndProvider>
    );
  };

  return (
    <>
      {body()}
      {/* Outside the view switch: the palette adds to the document, and the YAML and
          Testing views are looking at the same one. */}
      <ComponentPalette />
    </>
  );
}
