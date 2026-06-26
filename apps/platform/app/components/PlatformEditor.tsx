"use client";

import { useRef } from "react";
import { EditorRoot } from "@octo/editor";
import { useOrchestrator } from "@/app/run/OrchestratorContext";
import { orchestratorFileSystem } from "@/app/providers/orchestratorFileSystem";
import { bffRunTransport } from "@/app/run/transport";
import EditorHeader from "./EditorHeader";

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
}: {
  integrationId?: string;
  userMenu?: React.ReactNode;
}) {
  const { available } = useOrchestrator();
  // The authoritative integration id: seeded from the route and updated on save
  // (the first save mints it). TagButton reads it through getIntegrationId so it
  // never tags against a stale id captured before the save resolved.
  const idRef = useRef<string | null>(integrationId ?? null);
  return (
    <EditorRoot
      integrationId={integrationId}
      header={
        <EditorHeader
          userMenu={userMenu}
          getIntegrationId={() => idRef.current}
        />
      }
      fs={available ? orchestratorFileSystem : null}
      run={bffRunTransport}
      onSaved={(stored) => {
        idRef.current = stored.id;
        // Promote the address bar to the bookmarkable /platform/i/<id> URL
        // without remounting the editor (Next syncs the router for manual updates).
        window.history.replaceState(null, "", `/platform/i/${stored.id}`);
      }}
    />
  );
}
