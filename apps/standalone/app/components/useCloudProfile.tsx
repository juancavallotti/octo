"use client";

import { createElement, useEffect, useState } from "react";
import { ArrowDown, ArrowUp, ArrowUpDown, CircleCheck, Download, Workflow } from "lucide-react";
import type { PickerGroup, PickerItem, PickerRowAction } from "@octo/editor";
import { resolveIcon } from "@octo/editor/runtime";
import { paasBridge } from "../desktop";
import type { IntegrationSummary, PaasBridge, PlatformState, SyncState } from "../platform/types";

/** An integration's id in the picker, kept apart from the folder paths beside it. */
const PREFIX = "paas:";

/**
 * What the picker knows of the current installation's integrations: the last list it
 * has, from the shell's cache or a fresh answer, and why the last refresh failed when
 * it did. A list stays on show through a failed refresh, which is how it works offline.
 */
interface Remote {
  profile: string;
  list: IntegrationSummary[] | null;
  error?: string;
}

/**
 * A cloud profile's mark. Drawn here rather than taken from lucide, whose cloud is
 * lopsided enough to read as a smudge at heading size.
 */
function CloudMark({ size = 13 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2.2}
      strokeLinejoin="round"
      aria-hidden="true"
      className="shrink-0"
    >
      <path d="M6.5 19.5h11a4.25 4.25 0 0 0 .55-8.46A6.25 6.25 0 0 0 6.1 9.6 4.98 4.98 0 0 0 6.5 19.5Z" />
    </svg>
  );
}

/**
 * How a folder stands against its integration, as the icon at the end of its row. In
 * step is only a mark; the others are the button that brings it back into step.
 */
function syncMark(
  sync: SyncState,
  name: string,
  dir: string,
  paas: PaasBridge,
  busy: boolean,
): Pick<PickerItem, "badge" | "action"> {
  const action = (label: string, icon: React.ReactNode, direction: "push" | "pull" | "ask"): PickerRowAction => ({
    label,
    icon,
    onClick: () => void paas.sync(dir, direction).catch(() => {}),
    disabled: busy,
  });
  switch (sync) {
    case "synced":
      return { badge: <CircleCheck size={14} aria-label="In sync" className="mr-1 shrink-0 text-zinc-400" /> };
    case "ahead":
      return { action: action(`Push ${name}`, <ArrowUp size={14} className="text-sky-500" />, "push") };
    case "behind":
      return { action: action(`Pull ${name}`, <ArrowDown size={14} className="text-sky-500" />, "pull") };
    case "diverged":
      return { action: action(`${name} changed on both sides`, <ArrowUpDown size={14} className="text-amber-500" />, "ask") };
  }
}

export interface CloudProfile {
  /** The installation's integrations as a picker group; null without a platform. */
  group: PickerGroup | null;
  /** The open folder's row in that group, when it is one of them. */
  currentId?: string;
  /** Folders listed in the group, so the plain folder list can leave them out. */
  listed: Set<string>;
  /** Ask the platform again, keeping what is on show until it answers. */
  refresh(): void;
  /** Act on a picked id; false when it is not one of the group's. */
  select(id: string): boolean;
}

/**
 * The current installation's integrations for the folder picker: the shell's cached
 * list at once, refreshed in place, each marked with how its folder here stands.
 */
export function useCloudProfile(platform: PlatformState | null, vaultPath: string | null): CloudProfile {
  const paas = paasBridge();
  const [remote, setRemote] = useState<Remote | null>(null);
  const installation = platform?.profiles.find((p) => p.current);
  const profile = installation?.name;

  // The cached list as soon as the installation is known, so the open folder is
  // marked, and the menu filled, before anything asks the platform.
  useEffect(() => {
    const paas = paasBridge();
    if (!paas || !profile) return;
    let cancelled = false;
    paas
      .cachedIntegrations(profile)
      .then((list) => {
        if (!cancelled && list) setRemote((r) => (r?.profile === profile && r.list ? r : { profile, list }));
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [profile]);

  const mine = remote?.profile === profile ? remote : null;
  const loaded = mine?.list ?? [];
  const here = loaded.find((i) => i.local === vaultPath);
  const listed = new Set(loaded.flatMap((i) => (i.local ? [i.local] : [])));
  const select = (id: string) => {
    if (!id.startsWith(PREFIX)) return false;
    const i = loaded.find((x) => `${PREFIX}${x.id}` === id);
    if (i && paas && profile) {
      if (i.local) void paas.openLocal(i.local);
      else void paas.pull(profile, i.id, i.name).catch(() => {});
    }
    return true;
  };

  const refresh = () => {
    if (!paas || !profile) return;
    paas
      .integrations(profile)
      .then((list) => setRemote({ profile, list }))
      .catch((err: unknown) =>
        setRemote((r) => ({
          profile,
          list: r?.profile === profile ? r.list : null,
          error: err instanceof Error ? err.message : String(err),
        })),
      );
  };

  if (!paas || !installation || !profile) return { group: null, listed, refresh, select };

  const busy = platform?.activity?.state === "running";
  const items: PickerItem[] = loaded.map((i) => ({
    id: `${PREFIX}${i.id}`,
    name: i.name,
    // The icon it chose on the platform; one that chose none gets the generic glyph
    // the platform falls back to as well.
    icon: createElement(i.icon ? resolveIcon(i.icon) : Workflow, {
      size: 15,
      className: "shrink-0 text-zinc-400",
    }),
    hint: i.local ?? `Not on this machine — pick it to pull it from ${profile}`,
    ...(i.local && i.sync
      ? syncMark(i.sync, i.name, i.local, paas, busy)
      : {
          action: {
            label: `Pull ${i.name}`,
            icon: <Download size={14} className="text-zinc-400" />,
            onClick: () => void paas.pull(profile, i.id, i.name).catch(() => {}),
            disabled: busy,
          },
        }),
  }));

  const signedOut = installation.identity.state === "signed-out";
  const emptyLabel = mine?.error
    ? signedOut
      ? `Sign in to ${profile} to see its integrations`
      : mine.error
    : mine?.list
      ? "No integrations yet"
      : "Loading…";
  // A list on show that the platform could not refresh is the last one it gave.
  const stale = mine?.error && mine.list ? (signedOut ? " · signed out" : " · offline") : "";

  return {
    group: {
      id: profile,
      label: (
        <>
          <CloudMark />
          <span className="min-w-0 truncate">
            <b className="font-semibold text-zinc-700 dark:text-zinc-200">{profile}</b> cloud profile{stale}
          </span>
        </>
      ),
      items,
      emptyLabel,
    },
    ...(here ? { currentId: `${PREFIX}${here.id}` } : {}),
    listed,
    refresh,
    select,
  };
}
