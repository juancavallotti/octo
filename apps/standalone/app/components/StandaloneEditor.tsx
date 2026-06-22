"use client";

import { EditorRoot } from "@octo/editor";
import { localRunTransport } from "@/app/run/localRunTransport";
import StandaloneHeader from "./StandaloneHeader";

/**
 * Standalone wiring for the shared editor: supplies the local run transport
 * (backed by the bundled `octo` binary via @octo/run-host). The local-disk
 * filesystem capability is added next; until then this is the editor + RUN.
 */
export default function StandaloneEditor() {
  return (
    <EditorRoot run={localRunTransport} header={<StandaloneHeader />} />
  );
}
