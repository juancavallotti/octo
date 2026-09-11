"use client";

import { useEffect, useRef, useState } from "react";
import {
  CopyMcpUrlButton,
  DocumentRename,
  EditorRoot,
  setCapabilities,
  type Capabilities,
} from "@octo/editor";
import { subscribeIntegrationEvents } from "@octo/events";
import { useOrchestrator } from "@/app/run/OrchestratorContext";
import { useRoles } from "@/app/auth/RolesContext";
import { CAPABILITY_REASONS } from "@/app/auth/capabilities";
import { orchestratorFileSystem } from "@/app/providers/orchestratorFileSystem";
import { bffRunTransport } from "@/app/run/transport";
import { bffDevEnvStore } from "@/app/run/devEnvStore";
import { bffEditorMetaStore } from "@/app/run/editorMetaStore";
import { bffTestSuiteStore } from "@/app/run/testSuiteStore";
import { makeResourceStore } from "@/app/run/resourceStore";
import EditorHeader from "./EditorHeader";
import IntegrationNameChip from "./IntegrationNameChip";

/**
 * Platform wiring for the shared editor: supplies the orchestrator-backed
 * filesystem capability — but only once the orchestrator is reachable, so a
 * platform dev server without `ORCHESTRATOR_URL` shows just the editor + RUN —
 * and the BFF run transport. A client component so the capability objects never
 * cross the server/client boundary; the server-rendered account menu arrives as
 * a slot.
 */
export default function PlatformEditor({
  integrationId,
  userMenu,
  capabilities,
  mcpUrl,
}: {
  integrationId?: string;
  userMenu?: React.ReactNode;
  capabilities?: Capabilities | null;
  /**
   * The deployment's MCP endpoint (MCP_RESOURCE), for the console's copy button.
   * Configured rather than derived: this platform can sit behind a proxy on a
   * different host than the one the browser dialled.
   */
  mcpUrl?: string;
}) {
  // Inject the runtime schema (probed server-side from the octo binary) before
  // the editor's first render and before children read the palette. Synchronous
  // and idempotent; null leaves the empty bundled fallback in place.
  setCapabilities(capabilities);

  const { available } = useOrchestrator();
  const { can } = useRoles();
  // Read-only rather than absent, for somebody who may open an integration and
  // not change it. Absent would take the Testing tab and the resource panels with
  // it, and the point is that they can look.
  const fs = available
    ? can.build
      ? orchestratorFileSystem
      : { ...orchestratorFileSystem, readOnly: CAPABILITY_REASONS.build }
    : null;
  // The authoritative integration id: seeded from the route and updated on save
  // (the first save mints it). TagForm reads it through getIntegrationId so it
  // never tags against a stale id captured before the save resolved.
  const idRef = useRef<string | null>(integrationId ?? null);
  /**
   * One counter per kind of file, because the editor does a different thing with
   * each: the definition may need the user's say-so before it replaces unsaved work,
   * while a suite or a mock written elsewhere should simply appear.
   */
  const [reloadToken, setReloadToken] = useState(0);
  const [testsToken, setTestsToken] = useState(0);
  const [metaToken, setMetaToken] = useState(0);
  // Created once and kept across renders; onSaved pushes the minted id in so the
  // store keeps working after the first save without remounting the editor.
  const [resourceStore] = useState(() =>
    makeResourceStore(integrationId ?? null),
  );
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
      integrationId={integrationId}
      reloadToken={reloadToken}
      header={
        <EditorHeader
          userMenu={userMenu}
          getIntegrationId={() => idRef.current}
        />
      }
      files={
        <DocumentRename
          placeholder="untitled-integration"
          label="Rename integration"
        >
          <IntegrationNameChip />
        </DocumentRename>
      }
      consoleActions={mcpUrl ? <CopyMcpUrlButton url={mcpUrl} /> : undefined}
      fs={fs}
      run={bffRunTransport}
      devEnv={available ? bffDevEnvStore : null}
      resources={available ? resourceStore : null}
      meta={available ? bffEditorMetaStore : null}
      metaToken={metaToken}
      tests={available ? bffTestSuiteStore : null}
      testsToken={testsToken}
      onSaved={(stored) => {
        idRef.current = stored.id;
        resourceStore.setIntegrationId(stored.id);
        // Promote the address bar to the bookmarkable /platform/i/<id> URL
        // without remounting the editor (Next syncs the router for manual updates).
        window.history.replaceState(null, "", `/platform/i/${stored.id}`);
      }}
    />
  );
}
