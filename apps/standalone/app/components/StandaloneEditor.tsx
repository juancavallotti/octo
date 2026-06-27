"use client";

import { useEffect, useRef, useState } from "react";
import { EditorRoot } from "@octo/editor";
import { subscribeIntegrationEvents } from "@octo/events";
import { localRunTransport } from "@/app/run/localRunTransport";
import { localDiskFileSystem } from "@/app/providers/localDiskFileSystem";
import StandaloneHeader from "./StandaloneHeader";

/**
 * Standalone wiring for the shared editor: the local-disk filesystem capability
 * (load/save `*.yaml` flows under OCTO_FS_DIR) and the local run transport (the
 * bundled `octo` binary via @octo/run-host). `file` is the open flow id, taken
 * from the `?file=` query so the editor loads it on mount.
 */
export default function StandaloneEditor({ file }: { file?: string }) {
  // The id of the file currently open: the `?file=` query, or the (possibly
  // renamed) id adopted on save. Kept in a ref so the event subscription always
  // matches against the latest without resubscribing.
  const idRef = useRef<string | undefined>(file);
  useEffect(() => {
    idRef.current = file;
  }, [file]);

  // Bumped when the MCP server writes the file we have open, so the editor
  // live-reloads it (a clean editor silently, a dirty one via a banner).
  const [reloadToken, setReloadToken] = useState(0);
  useEffect(
    () =>
      subscribeIntegrationEvents((event) => {
        if (event.id === idRef.current) setReloadToken((n) => n + 1);
      }),
    [],
  );

  return (
    <EditorRoot
      integrationId={file}
      reloadToken={reloadToken}
      fs={localDiskFileSystem}
      run={localRunTransport}
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
