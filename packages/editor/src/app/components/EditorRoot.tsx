"use client";

import { EditorStateProvider } from "../state/editorState";
import {
  FileSystemProvider,
  type FileSystemCapability,
  type StoredDocument,
} from "../providers/FileSystemProvider";
import { SaveProvider } from "../save/SaveContext";
import { RunProvider } from "../run/RunContext";
import { FlowRunProvider } from "../run/FlowRunContext";
import { ConsoleProvider } from "../run/console";
import type { RunTransport } from "../run/transport";
import { DevEnvStoreProvider, type DevEnvStore } from "../state/devEnvStore";
import {
  ResourceStoreProvider,
  type ResourceStore,
} from "../providers/ResourceStoreProvider";
import {
  EditorMetaProvider,
  type EditorMetaStore,
} from "../providers/EditorMetaProvider";
import IntegrationLoader from "./IntegrationLoader";
import LogPanel from "./LogPanel";
import EditorBody from "./EditorBody";

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
  devEnv,
  resources,
  meta,
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
  /** Dev-env capability backing the Dev .env panel; omit to disable it. */
  devEnv?: DevEnvStore | null;
  /** Resource-store capability backing the Resources tab; omit to hide the tab. */
  resources?: ResourceStore | null;
  /**
   * Editor-meta capability (`.octo/editor-meta.json`), holding a flow's saved test
   * inputs. Omit and inputs still work — they just live for the session.
   */
  meta?: EditorMetaStore | null;
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

        {/* Body: the canvas or YAML preview (per view mode) above the logs */}
        <div className="flex flex-1 min-h-0 flex-col">
          <EditorBody />
          <LogPanel />
        </div>
      </div>
    </>
  );

  // Wrap in the capability providers only when supplied, so absence is structural
  // (the consuming controls read a null context and render nothing). The save
  // controller sits inside the filesystem provider it depends on.
  if (resources)
    tree = (
      <ResourceStoreProvider value={resources}>{tree}</ResourceStoreProvider>
    );
  if (run) {
    // FlowRunProvider sits inside RunProvider and drives the console (it opens the tab
    // that answers what the user just asked), so the console provider wraps them both.
    tree = (
      <RunProvider transport={run}>
        <FlowRunProvider transport={run}>
          <DevEnvStoreProvider value={devEnv ?? null}>{tree}</DevEnvStoreProvider>
        </FlowRunProvider>
      </RunProvider>
    );
  }
  if (fs)
    tree = (
      <FileSystemProvider value={fs}>
        <SaveProvider onSaved={onSaved}>{tree}</SaveProvider>
      </FileSystemProvider>
    );

  // Meta is mounted even without a store: test inputs still work for an unsaved draft,
  // they simply are not written down (the provider reports canPersist: false). It reads
  // the document, so it sits inside the state provider.
  return (
    <EditorStateProvider>
      <EditorMetaProvider store={meta ?? null}>
        <ConsoleProvider>{tree}</ConsoleProvider>
      </EditorMetaProvider>
    </EditorStateProvider>
  );
}
