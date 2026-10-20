"use client";

import { useEffect, useState } from "react";
import { Cloud, CloudDownload, FolderSearch } from "lucide-react";
import { WorkspacePicker } from "@octo/editor";
import { desktopBridge, type VaultRef } from "../desktop";

/**
 * Which folder is being served, and how to change it. Rendered only when a shell is
 * hosting the page: choosing a folder needs a native picker and a restart, neither of
 * which a browser can do, and a plain browser's folder is chosen by whoever started
 * the server.
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

  // Nothing to show until the bridge answers, which also keeps the first client
  // render identical to the server's.
  if (!bridge || !vault) return null;

  return (
    <WorkspacePicker
      label="Project folder"
      current={vault.name}
      currentHint={vault.path}
      // Refreshed when the menu opens, so a folder opened elsewhere shows up here.
      onOpen={() => {
        bridge
          .recents()
          .then(setRecents)
          .catch(() => {});
      }}
      items={recents.map((v) => ({
        id: v.path,
        name: v.name,
        hint: v.platform ? `${v.path} — ${v.platform}` : v.path,
        ...(v.platform
          ? { badge: <Cloud size={13} aria-label={v.platform} className="shrink-0 text-sky-500" /> }
          : {}),
      }))}
      onSelect={(path) => void bridge.switchVault(path)}
      actions={[
        {
          label: "Open folder…",
          icon: <FolderSearch size={16} className="shrink-0 text-zinc-400" />,
          onClick: () => void bridge.pickVault(),
        },
        // Absent on a shell older than this page.
        ...(bridge.paas
          ? [
              {
                label: "From the platform…",
                icon: <CloudDownload size={16} className="shrink-0 text-zinc-400" />,
                onClick: () => void bridge.paas?.open("integrations"),
              },
            ]
          : []),
      ]}
      searchPlaceholder="Search folders…"
      emptyLabel="No other folders yet"
    />
  );
}
