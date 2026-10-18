"use client";

import { useEffect, useState } from "react";
import {
  DocumentRename,
  EditorRoot,
  setCapabilities,
  type Capabilities,
} from "@octo/editor";
import { subscribeIntegrationEvents } from "@octo/events";
import { localRunTransport } from "@/app/run/localRunTransport";
import { localDevEnvStore } from "@/app/run/devEnvStore";
import { localDiskResourceStore } from "@/app/run/resourceStore";
import { localEditorMetaStore } from "@/app/run/editorMetaStore";
import { localTestSuiteStore } from "@/app/run/testSuiteStore";
import { localDiskFileSystem } from "@/app/providers/localDiskFileSystem";
import McpCopyAction from "./McpCopyAction";
import ProjectName from "./ProjectName";
import StandaloneHeader from "./StandaloneHeader";
import { useDesktopPrefs } from "./useDesktopPrefs";

/**
 * This app's wiring for the shared editor: the local-disk filesystem capability and
 * the local run transport. The project is the folder the app serves, `projectId` its
 * name; `file` is the config file to open first, taken from the `?file=` query.
 * `capabilities` is the runtime schema the server probed — null when no runner is
 * configured — injected before the editor renders so the palette derives from it.
 */
export default function StandaloneEditor({
  projectId,
  file,
  capabilities,
}: {
  projectId: string;
  file?: string;
  capabilities?: Capabilities | null;
}) {
  // Inject the runtime-generated schema before the editor's first render (and
  // before children read the palette). Synchronous and idempotent; a null value
  // leaves the bundled fallback in place.
  setCapabilities(capabilities);

  /**
   * One counter per kind of file, because the editor does a different thing with
   * each: the definition may need the user's say-so before it replaces unsaved work,
   * while a suite or a mock written elsewhere should simply appear.
   */
  // Null when nothing is hosting the page, where the editor uses its own defaults.
  const prefs = useDesktopPrefs();

  const [reloadToken, setReloadToken] = useState(0);
  const [testsToken, setTestsToken] = useState(0);
  const [metaToken, setMetaToken] = useState(0);
  useEffect(
    () =>
      subscribeIntegrationEvents((event) => {
        if (event.id !== projectId) return;
        if (event.type === "integration.tests-updated") setTestsToken((n) => n + 1);
        else if (event.type === "integration.meta-updated") setMetaToken((n) => n + 1);
        else setReloadToken((n) => n + 1);
      }),
    [projectId],
  );

  return (
    <EditorRoot
      projectId={projectId}
      initialFile={file}
      reloadToken={reloadToken}
      fs={localDiskFileSystem}
      run={localRunTransport}
      devEnv={localDevEnvStore}
      resources={localDiskResourceStore}
      meta={localEditorMetaStore}
      metaToken={metaToken}
      tests={localTestSuiteStore}
      testsToken={testsToken}
      prefs={prefs}
      header={<StandaloneHeader />}
      files={
        <DocumentRename placeholder="untitled-project" label="Rename project">
          <ProjectName />
        </DocumentRename>
      }
      consoleActions={<McpCopyAction />}
    />
  );
}
