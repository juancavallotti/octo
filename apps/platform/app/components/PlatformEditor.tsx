"use client";

import { useOrchestrator } from "@/app/run/OrchestratorContext";
import { orchestratorFileSystem } from "@/app/providers/orchestratorFileSystem";
import { bffRunTransport } from "@/app/run/transport";
import EditorRoot from "./EditorRoot";

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
  return (
    <EditorRoot
      integrationId={integrationId}
      userMenu={userMenu}
      fs={available ? orchestratorFileSystem : null}
      run={bffRunTransport}
    />
  );
}
