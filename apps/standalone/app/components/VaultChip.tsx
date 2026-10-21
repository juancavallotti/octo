"use client";

import { useEffect, useState } from "react";
import { CircleAlert, Download, Folder, FolderSearch, Loader2 } from "lucide-react";
import { WorkspacePicker } from "@octo/editor";
import { desktopBridge, paasBridge, type VaultRef } from "../desktop";
import { usePlatformState } from "../platform/usePlatformState";
import { useCloudProfile } from "./useCloudProfile";

/**
 * Which folder is being served, and how to change it. Rendered only when a shell is
 * hosting the page: choosing a folder needs a native picker and a restart, neither of
 * which a browser can do, and a plain browser's folder is chosen by whoever started
 * the server.
 *
 * Under a shell that drives the platform it also lists the current installation's
 * integrations, and carries the running platform operation on its icon.
 */
export default function VaultChip() {
  const bridge = desktopBridge();
  const paas = paasBridge();
  const platform = usePlatformState();
  const [vault, setVault] = useState<VaultRef | null>(null);
  const [recents, setRecents] = useState<VaultRef[]>([]);
  const cloud = useCloudProfile(platform, vault?.path ?? null);

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

  const busy = platform?.activity?.state === "running";
  const failed = platform?.activity?.state === "failed" ? platform.activity : null;
  // A linked folder goes by its integration's name, which the folder's own need not be.
  // The platform state is read first: it follows a link made while the folder is open.
  const name = platform?.link?.integrationName ?? vault.integration?.name ?? vault.name;

  return (
    <WorkspacePicker
      label="Project folder"
      icon={
        busy ? (
          <Loader2 size={14} className="shrink-0 animate-spin text-sky-500" />
        ) : failed ? (
          <CircleAlert size={14} className="shrink-0 text-rose-500" />
        ) : undefined
      }
      current={name}
      currentHint={
        busy
          ? `${platform?.activity?.label}…`
          : failed
            ? `${failed.label} failed: ${failed.message ?? ""}`
            : vault.path
      }
      {...(cloud.currentId ? { currentId: cloud.currentId } : {})}
      // Refreshed when the menu opens, so a folder opened elsewhere, or an integration
      // changed on the platform, shows up here.
      onOpen={() => {
        bridge
          .recents()
          .then(setRecents)
          .catch(() => {});
        cloud.refresh();
      }}
      items={recents
        .filter((v) => !cloud.listed.has(v.path))
        .map((v) => ({
          id: v.path,
          name: v.integration?.name ?? v.name,
          icon: <Folder size={15} className="shrink-0 text-zinc-400" />,
          hint: v.integration ? `${v.path} — on ${v.integration.profile}` : v.path,
        }))}
      groups={cloud.group ? [cloud.group] : []}
      onSelect={(id) => {
        if (!cloud.select(id)) void bridge.switchVault(id);
      }}
      actions={[
        {
          label: "Open folder…",
          icon: <FolderSearch size={16} className="shrink-0 text-zinc-400" />,
          onClick: () => void bridge.pickVault(),
        },
        // Absent on a shell older than this page.
        ...(paas
          ? [
              {
                label: "From the platform…",
                icon: <Download size={16} className="shrink-0 text-zinc-400" />,
                onClick: () => void paas.open("integrations"),
              },
            ]
          : []),
      ]}
      searchPlaceholder="Search folders and integrations…"
      emptyLabel="No other folders yet"
    />
  );
}
