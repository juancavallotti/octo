"use client";

import { useEffect, useState, useSyncExternalStore } from "react";
import { paasBridge } from "../../desktop";
import { tabFor, type PlatformTab } from "../types";
import { usePlatformState } from "../usePlatformState";
import DeploymentsTab from "./DeploymentsTab";
import FolderTab from "./FolderTab";
import IntegrationsTab from "./IntegrationsTab";
import LogsTab from "./LogsTab";
import OutputDrawer from "./OutputDrawer";
import PlatformHeader from "./PlatformHeader";
import SettingsTab from "./SettingsTab";

const subscribe = () => () => {};
const hasBridge = () => paasBridge() !== null;

/**
 * The Platform window: a header like the editor's — the mark, which installation, the
 * tabs — over the part of the platform a tab shows, and the running operation's
 * output beneath. The installation shown is octopaas's current one, the same one the
 * Platform menu and the user's terminal act on; choosing another here switches it.
 */
export default function PlatformApp({ focus }: { focus?: string }) {
  const desktop = useSyncExternalStore(subscribe, hasBridge, () => true);
  const state = usePlatformState();
  const [tab, setTab] = useState<PlatformTab>(tabFor(focus));

  useEffect(() => {
    const paas = paasBridge();
    if (!paas) return;
    void paas.refresh().catch(() => {});
    return paas.onFocus((f) => setTab(tabFor(f)));
  }, []);

  if (!desktop) {
    return (
      <main className="flex h-full items-center justify-center p-8 text-sm text-zinc-500">
        The platform is available in Octo Desktop, which runs the octopaas CLI for you.
      </main>
    );
  }

  const profile = state?.profiles.find((p) => p.current)?.name ?? state?.profiles[0]?.name ?? null;
  const use = (name: string) => void paasBridge()?.useProfile(name).catch(() => {});

  return (
    <div className="flex h-full flex-col">
      <PlatformHeader state={state} profile={profile} onProfile={use} tab={tab} onTab={setTab} />
      <main className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex max-w-4xl flex-col gap-4 p-6">
          {state?.cliError && (
            <p role="alert" className="rounded-lg bg-rose-500/10 p-3 text-sm text-rose-700 dark:text-rose-300">
              octopaas could not be run: {state.cliError}. Check its path in Settings.
            </p>
          )}
          {state && tab === "integrations" && (
            <IntegrationsTab state={state} profile={profile} onManage={() => setTab("settings")} />
          )}
          {state && tab === "folder" && <FolderTab state={state} profile={profile} />}
          {state && tab === "deployments" && <DeploymentsTab state={state} />}
          {state && tab === "logs" && <LogsTab state={state} />}
          {state && tab === "settings" && <SettingsTab state={state} />}
        </div>
      </main>
      <OutputDrawer activity={state?.activity ?? null} />
    </div>
  );
}
