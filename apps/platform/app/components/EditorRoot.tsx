"use client";

import { EditorStateProvider } from "@/app/state/editorState";
import {
  FileSystemProvider,
  type FileSystemCapability,
} from "@/app/providers/FileSystemProvider";
import { RunProvider } from "@/app/run/RunContext";
import type { RunTransport } from "@/app/run/transport";
import DndProvider from "./DndProvider";
import Sidebar from "./Sidebar";
import Canvas from "./Canvas";
import SettingsPanel from "./SettingsPanel";
import EditorHeader from "./EditorHeader";
import IntegrationLoader from "./IntegrationLoader";
import LogPanel from "./LogPanel";

/**
 * EditorRoot is the top-level layout for the Octo visual editor: a thin top bar
 * (with the RUN control), a left component sidebar, the main flow canvas, and a
 * bottom runner-log panel. It always owns editor-wide state (EditorStateProvider)
 * and the drag-and-drop session; the load/save (`fs`) and run (`run`) capabilities
 * are optional — when one is supplied the editor wraps the tree in its provider
 * and the matching controls appear, and when it is omitted those controls render
 * nothing. This is what lets the same editor embed in the orchestrator-backed
 * platform, a local standalone app, or a read-only preview.
 *
 * `userMenu` is an app-owned slot (e.g. a server-rendered account menu); `loader`
 * is an extra in-provider slot used by the dev-only `/preview` route to inject its
 * own sample loader.
 */
export default function EditorRoot({
  integrationId,
  loader,
  userMenu,
  fs,
  run,
}: {
  integrationId?: string;
  loader?: React.ReactNode;
  userMenu?: React.ReactNode;
  /** Load/save capability; omit for a read-only editor (no Save / loader). */
  fs?: FileSystemCapability | null;
  /** Run capability; omit to hide the RUN control and log panel. */
  run?: RunTransport | null;
}) {
  let tree = (
    <>
      <IntegrationLoader integrationId={integrationId} />
      {loader}
      <div className="flex flex-1 flex-col h-full">
        {/* Top bar — userMenu is a server-rendered slot. */}
        <EditorHeader userMenu={userMenu} />

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
    </>
  );

  // Wrap in the capability providers only when supplied, so absence is structural
  // (the consuming controls read a null context and render nothing).
  if (run) tree = <RunProvider transport={run}>{tree}</RunProvider>;
  if (fs) tree = <FileSystemProvider value={fs}>{tree}</FileSystemProvider>;

  return <EditorStateProvider>{tree}</EditorStateProvider>;
}
