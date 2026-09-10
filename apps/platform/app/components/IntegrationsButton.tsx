"use client";

import { useRouter } from "next/navigation";
import { LayoutGrid } from "lucide-react";

/**
 * Editor-header button that opens the integration management route. It says
 * "Manage" rather than "Integrations": which integration you are on is the
 * picker's job at the other end of the bar, and this is the verb — deep-linking
 * to the integration currently being edited (so you land on it in context), or the
 * manager root when editing an unsaved document. The id is read at click time via
 * getIntegrationId so a just-minted id (from the first save) is honoured, matching
 * how TagForm reads it.
 */
export default function IntegrationsButton({
  getIntegrationId,
}: {
  /** Reads the authoritative integration id (updated on save), or null if unsaved. */
  getIntegrationId?: () => string | null;
}) {
  const router = useRouter();
  const open = () => {
    const id = getIntegrationId?.() ?? null;
    router.push(
      id ? `/platform/integrations/i/${encodeURIComponent(id)}` : "/platform/integrations",
    );
  };
  return (
    <button
      type="button"
      onClick={open}
      title="Manage integrations"
      className="inline-flex items-center gap-1.5 rounded-md border border-black/10 px-3 py-1 text-sm font-medium text-zinc-600 transition-colors hover:bg-black/[0.04] hover:text-zinc-900 dark:border-white/15 dark:text-zinc-300 dark:hover:bg-white/[0.06] dark:hover:text-zinc-100"
    >
      <LayoutGrid className="h-3.5 w-3.5" />
      Manage
    </button>
  );
}
