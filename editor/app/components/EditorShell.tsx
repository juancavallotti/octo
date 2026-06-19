import Image from "next/image";
import { EditorStateProvider } from "@/app/state/editorState";
import { RunProvider } from "@/app/run/RunContext";
import DndProvider from "./DndProvider";
import Sidebar from "./Sidebar";
import Canvas from "./Canvas";
import SettingsPanel from "./SettingsPanel";
import RunBar from "./RunBar";
import LogPanel from "./LogPanel";

/**
 * EditorShell is the top-level layout for the Octo visual editor: a thin top
 * bar (with the RUN control), a left component sidebar, the main flow canvas,
 * and a bottom runner-log panel. As a "large" component it owns editor-wide
 * state via a reducer (EditorStateProvider) and the run lifecycle (RunProvider).
 */
export default function EditorShell() {
  return (
    <EditorStateProvider>
      <RunProvider>
        <div className="flex flex-1 flex-col h-full">
          {/* Top bar */}
          <header className="flex items-center gap-2 border-b border-black/10 dark:border-white/10 px-4 h-12 shrink-0">
            {/* h-6 w-auto controls both axes so Tailwind's `img { height: auto }`
                reset doesn't trigger Next's aspect-ratio warning. */}
            <Image
              src="/octo-logo.png"
              alt="Octo logo"
              width={24}
              height={24}
              className="h-6 w-auto"
              priority
            />
            <span className="font-semibold tracking-tight">Octo</span>
            <RunBar />
          </header>

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
