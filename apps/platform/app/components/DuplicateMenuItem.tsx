"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Copy } from "lucide-react";
import { useSave } from "@octo/editor";
import { useRoles } from "@/app/auth/RolesContext";
import { CAPABILITY_REASONS } from "@/app/auth/capabilities";
import { createIntegration, getIntegration } from "@/app/model/orchestrator";

/**
 * Duplicates the current integration into a fresh "Copy of …" record and opens the
 * copy in the editor. Like {@link TagForm} it saves first (so the copy captures
 * what's on screen) and reads the authoritative id from `getIntegrationId` — a ref
 * the host updates on save — before cloning the saved definition. Renders nothing
 * without a filesystem capability, and is disabled while there's nothing worth
 * persisting yet (empty document).
 *
 * A row in the header's overflow menu rather than a button on the bar: it is a
 * once-in-a-while action, and it was taking space next to the ones that are not.
 */
export default function DuplicateMenuItem({
  getIntegrationId,
  onDone,
}: {
  getIntegrationId: () => string | null;
  /** Closes the menu this row lives in. */
  onDone?: () => void;
}) {
  const save = useSave();
  const { can } = useRoles();
  const router = useRouter();
  const [busy, setBusy] = useState(false);

  // No filesystem capability => nothing to duplicate (mirrors how Save hides).
  if (!save) return null;

  const duplicate = async () => {
    if (busy || save.empty) return;
    setBusy(true);
    try {
      // Save first so the copy captures the on-screen definition; on the first save
      // this mints the id we then read via the ref.
      await save.save();
      const id = getIntegrationId();
      if (!id) return;
      const source = await getIntegration(id);
      const created = await createIntegration({
        name: `Copy of ${source.name}`,
        definition: source.definition,
      });
      onDone?.();
      router.push(`/platform/i/${encodeURIComponent(created.id)}`);
    } finally {
      setBusy(false);
    }
  };

  return (
    <button
      type="button"
      onClick={duplicate}
      disabled={save.empty || busy || !can.build}
      title={
        !can.build
          ? CAPABILITY_REASONS.build
          : save.empty
            ? "Nothing to duplicate yet"
            : "Duplicate this integration"
      }
      className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm transition-colors hover:bg-black/[0.04] disabled:opacity-50 dark:hover:bg-white/[0.06]"
    >
      <Copy size={15} className="shrink-0 text-zinc-400" />
      {busy ? "Duplicating…" : "Duplicate"}
    </button>
  );
}
