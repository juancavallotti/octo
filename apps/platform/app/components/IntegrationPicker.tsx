"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import {
  WorkspacePicker,
  useEditorState,
  useFileSystem,
  type StoredDocument,
} from "@octo/editor";

/**
 * Which integration is open, and a way to switch to another one.
 *
 * The same control the desktop shell uses for its folder, with the meaning the
 * platform has for it: the platform serves one orchestrator, not a directory the
 * user chose, so the thing worth switching between here is the integration. Picking
 * one navigates to its editor route; there is no "Open folder…" row, because there
 * is no folder to open.
 *
 * The list is read through the filesystem capability, the same call the standalone's
 * file switcher makes — so it is the orchestrator's own list, not a second one.
 */
export default function IntegrationPicker() {
  const fs = useFileSystem();
  const { state } = useEditorState();
  const router = useRouter();
  const [items, setItems] = useState<StoredDocument[]>([]);

  if (!fs?.list) return null;

  const current = state.integration.id;

  return (
    <WorkspacePicker
      label="Integration"
      current={state.integration.name || "Untitled integration"}
      // Refreshed on open rather than once, so an integration created elsewhere
      // (or the one just saved) shows up without a reload.
      onOpen={() => {
        fs.list?.()
          .then(setItems)
          .catch(() => {});
      }}
      items={items
        .filter((d) => d.id !== current)
        .map((d) => ({ id: d.id, name: d.name }))}
      onSelect={(id) => router.push(`/platform/i/${encodeURIComponent(id)}`)}
      searchPlaceholder="Search integrations…"
      emptyLabel="No other integrations yet"
    />
  );
}
