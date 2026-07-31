"use client";

import { useEffect, useRef, useState } from "react";
import { EditorRoot, setCapabilities, type Capabilities } from "@octo/editor";
import { subscribeIntegrationEvents } from "@octo/events";
import { localRunTransport } from "@/app/run/localRunTransport";
import { localDevEnvStore } from "@/app/run/devEnvStore";
import { localDiskResourceStore } from "@/app/run/resourceStore";
import { localEditorMetaStore } from "@/app/run/editorMetaStore";
import { localTestSuiteStore } from "@/app/run/testSuiteStore";
import { localDiskFileSystem } from "@/app/providers/localDiskFileSystem";
import StandaloneHeader from "./StandaloneHeader";

/**
 * Standalone wiring for the shared editor: the local-disk filesystem capability
 * (load/save `*.yaml` flows under OCTO_FS_DIR) and the local run transport (the
 * bundled `octo` binary via @octo/run-host). `file` is the open flow id, taken
 * from the `?file=` query so the editor loads it on mount. `capabilities` is the
 * runtime-generated schema the server probed (null when no runner is configured);
 * it's injected before the editor renders so the palette derives from it.
 */
export default function StandaloneEditor({
  file,
  capabilities,
}: {
  file?: string;
  capabilities?: Capabilities | null;
}) {
  // Inject the runtime-generated schema before the editor's first render (and
  // before children read the palette). Synchronous and idempotent; a null value
  // leaves the bundled fallback in place.
  setCapabilities(capabilities);

  // The id of the file currently open: the `?file=` query, or the (possibly
  // renamed) id adopted on save. Kept in a ref so the event subscription always
  // matches against the latest without resubscribing.
  const idRef = useRef<string | undefined>(file);
  useEffect(() => {
    idRef.current = file;
  }, [file]);

  /**
   * One counter per kind of file, because the editor does a different thing with
   * each: the definition may need the user's say-so before it replaces unsaved work,
   * while a suite or a mock written elsewhere should simply appear.
   */
  const [reloadToken, setReloadToken] = useState(0);
  const [testsToken, setTestsToken] = useState(0);
  const [metaToken, setMetaToken] = useState(0);
  useEffect(
    () =>
      subscribeIntegrationEvents((event) => {
        if (event.id !== idRef.current) return;
        if (event.type === "integration.tests-updated") setTestsToken((n) => n + 1);
        else if (event.type === "integration.meta-updated") setMetaToken((n) => n + 1);
        else setReloadToken((n) => n + 1);
      }),
    [],
  );

  return (
    <EditorRoot
      integrationId={file}
      reloadToken={reloadToken}
      fs={localDiskFileSystem}
      run={localRunTransport}
      devEnv={localDevEnvStore}
      resources={localDiskResourceStore}
      meta={localEditorMetaStore}
      metaToken={metaToken}
      tests={localTestSuiteStore}
      testsToken={testsToken}
      header={<StandaloneHeader />}
      onSaved={(stored) => {
        // Reflect the open file in the URL so a reload reopens it; the header
        // reads the current id from editor state, so no remount is needed.
        idRef.current = stored.id;
        window.history.replaceState(
          null,
          "",
          `/?file=${encodeURIComponent(stored.id)}`,
        );
      }}
    />
  );
}
