"use client";

import { EditorRoot } from "@octo/editor";
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
  return (
    <EditorRoot
      integrationId={file}
      fs={localDiskFileSystem}
      run={localRunTransport}
      header={<StandaloneHeader current={file} />}
    />
  );
}
