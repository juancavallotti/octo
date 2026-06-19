import { EditorStateProvider } from "@/app/state/editorState";
import { RunProvider } from "@/app/run/RunContext";
import DndProvider from "./DndProvider";
import Sidebar from "./Sidebar";
import Canvas from "./Canvas";
import SettingsPanel from "./SettingsPanel";
import EditorHeader from "./EditorHeader";
import IntegrationLoader from "./IntegrationLoader";
import LogPanel from "./LogPanel";

/**
 * EditorShell is the top-level layout for the Octo visual editor: a thin top
 * bar (with the RUN control), a left component sidebar, the main flow canvas,
 * and a bottom runner-log panel. As a "large" component it owns editor-wide
 * state via a reducer (EditorStateProvider) and the run lifecycle (RunProvider).
 *
 * When rendered from the `/i/[id]` route it receives that integration id and
 * loads it into the editor (see IntegrationLoader); the bare `/` route opens a
 * fresh document.
 */
export default function EditorShell({
  integrationId,
}: {
  integrationId?: string;
}) {
  return (
    <EditorStateProvider>
      <RunProvider>
        <IntegrationLoader integrationId={integrationId} />
        <div className="flex flex-1 flex-col h-full">
          {/* Top bar */}
          <EditorHeader />

          {/* Body: sidebar + canvas (one drag-and-drop session) above the logs */}
          <div className="flex flex-1 min-h-0 flex-col">
            <DndProvider>
              <div className="flex flex-1 min-h-0">
                <Sidebar />
                <Canvas />
                <SettingsPanel />
              </div>
            </DndProvider>
            <LogPanel />
          </div>
        </div>
      </RunProvider>
    </EditorStateProvider>
  );
}
