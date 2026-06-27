"use client";

import { EditorStateProvider } from "../state/editorState";
import {
  FileSystemProvider,
  type FileSystemCapability,
  type StoredDocument,
} from "../providers/FileSystemProvider";
import { SaveProvider } from "../save/SaveContext";
import { RunProvider } from "../run/RunContext";
import type { RunTransport } from "../run/transport";
import DndProvider from "./DndProvider";
import Sidebar from "./Sidebar";
import Canvas from "./Canvas";
import SettingsPanel from "./SettingsPanel";
import IntegrationLoader from "./IntegrationLoader";
import LogPanel from "./LogPanel";

/**
 * EditorRoot is the embeddable Octo visual editor: a top bar, a left component
 * sidebar, the main flow canvas, and a bottom runner-log panel. It always owns
 * editor-wide state (EditorStateProvider) and the drag-and-drop session; the
 * load/save (`fs`) and run (`run`) capabilities are optional — when one is
 * supplied the editor wraps the tree in its provider and the matching controls
 * appear, and when it is omitted those controls render nothing. This is what lets
 * the same editor embed in the orchestrator-backed platform, a local standalone
 * app, or a read-only preview.
 *
 * The top bar is the app-owned `header` slot (it composes the controls — Save,
 * folders, RUN, account menu — that make sense for that host). `loader` is an
 * extra in-provider slot used by a preview route to inject its own sample loader.
 */
export default function EditorRoot({
  integrationId,
  reloadToken,
  loader,
  header,
  fs,
  run,
  onSaved,
}: {
  integrationId?: string;
  /**
   * Bumped by the host to request a live reload of the open file after an
   * external write (see @octo/events); a clean editor reloads silently, a dirty
   * one shows a reload banner. Omit when the host has no event stream.
   */
  reloadToken?: string | number;
  loader?: React.ReactNode;
  /** App-owned top bar; composes editor controls (e.g. via PlatformEditor). */
  header?: React.ReactNode;
  /** Load/save capability; omit for a read-only editor (no Save / loader). */
  fs?: FileSystemCapability | null;
  /** Run capability; omit to hide the RUN control and log panel. */
  run?: RunTransport | null;
  /** Called after a save with the stored record (e.g. to update the URL). */
  onSaved?: (stored: StoredDocument) => void;
}) {
  let tree = (
    <>
      {loader}
      <div className="flex flex-1 flex-col h-full">
        {header}
        {/* The loader lives under the header so its external-write reload banner
            appears between the top bar and the canvas. */}
        <IntegrationLoader
          integrationId={integrationId}
          reloadToken={reloadToken}
        />

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
  // (the consuming controls read a null context and render nothing). The save
  // controller sits inside the filesystem provider it depends on.
  if (run) tree = <RunProvider transport={run}>{tree}</RunProvider>;
  if (fs)
    tree = (
      <FileSystemProvider value={fs}>
        <SaveProvider onSaved={onSaved}>{tree}</SaveProvider>
      </FileSystemProvider>
    );

  return <EditorStateProvider>{tree}</EditorStateProvider>;
}
