"use client";

import { useEffect, useState } from "react";
import { FolderSearch } from "lucide-react";
import { WorkspacePicker } from "@octo/editor";
import { desktopBridge, type VaultRef } from "../desktop";

/**
 * Which folder the desktop shell is serving, and how to change it.
 *
 * Desktop only. Choosing a folder is a shell capability — only Electron can open a
 * native picker or restart itself on another directory — and in a browser the chip
 * was a dead label for something the user could not act on. `task dev` and the
 * Docker image are configured by whoever started them, not from in here.
 *
 * The chip and its menu are the shared WorkspacePicker; the platform shows the same
 * control over its integrations. Only the two shell-specific parts live here: the
 * recents list, and "Open folder…".
 */
export default function VaultChip() {
  const bridge = desktopBridge();
  const [vault, setVault] = useState<VaultRef | null>(null);
  const [recents, setRecents] = useState<VaultRef[]>([]);

  useEffect(() => {
    if (!bridge) return;
    let cancelled = false;
    bridge
      .vault()
      .then((v) => {
        if (!cancelled) setVault(v);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [bridge]);

  // Nothing to show until the shell answers — which also keeps the first client
  // render identical to the server's, so hydration has nothing to disagree about.
  if (!bridge || !vault) return null;

  return (
    <WorkspacePicker
      label="Project folder"
      current={vault.name}
      currentHint={vault.path}
      // Refreshed when the menu opens rather than once, so a folder opened via the
      // app menu shows up without a reload.
      onOpen={() => {
        bridge
          .recents()
          .then(setRecents)
          .catch(() => {});
      }}
      items={recents.map((v) => ({ id: v.path, name: v.name, hint: v.path }))}
      onSelect={(path) => void bridge.switchVault(path)}
      action={{
        label: "Open folder…",
        icon: <FolderSearch size={16} className="shrink-0 text-zinc-400" />,
        onClick: () => void bridge.pickVault(),
      }}
      searchPlaceholder="Search folders…"
      emptyLabel="No other folders yet"
    />
  );
}
