"use client";

import Link from "next/link";
import { useOrchestrator } from "@/app/run/OrchestratorContext";
import IntegrationsManager from "@/app/components/integrations/IntegrationsManager";

/**
 * The integration management route. Guarded on orchestrator availability: with no
 * `ORCHESTRATOR_URL` configured there is nothing to manage, so it explains how to
 * enable the feature and links back to the editor.
 */
export default function IntegrationsPage() {
  const { available, ready } = useOrchestrator();

  if (!ready) return null;

  if (!available) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 px-6 text-center">
        <p className="text-sm text-zinc-500">
          Integration management is unavailable. Set{" "}
          <code className="rounded bg-black/[0.06] px-1 dark:bg-white/10">
            ORCHESTRATOR_URL
          </code>{" "}
          to enable it.
        </p>
        <Link href="/" className="text-sm text-sky-600 hover:underline">
          Back to editor
        </Link>
      </div>
    );
  }

  return <IntegrationsManager />;
}
