"use client";

import { useEffect, useState, useSyncExternalStore } from "react";
import { TriangleAlert } from "lucide-react";
import { paasBridge } from "../../desktop";
import { tabFor, type PlatformTab } from "../types";
import { cleanMessage } from "../useAction";
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
  // Why the last switch of installation or environment was refused. These calls do
  // not run as operations, so the output drawer would say nothing of them.
  const [failure, setFailure] = useState<string | null>(null);

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
  const report = (err: unknown) => setFailure(cleanMessage(err instanceof Error ? err.message : String(err)));
  const use = (name: string) => {
    setFailure(null);
    void paasBridge()?.useProfile(name).catch(report);
  };
  const useEnvironment = (environment: string) => {
    if (!profile) return;
    setFailure(null);
    void paasBridge()?.useEnvironment(profile, environment).catch(report);
  };

  return (
    <div className="flex h-full flex-col">
      <PlatformHeader
        state={state}
        profile={profile}
        onProfile={use}
        onEnvironment={useEnvironment}
        tab={tab}
        onTab={setTab}
      />
      <main className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex max-w-4xl flex-col gap-4 p-6">
          {state?.cliError && (
            <p role="alert" className="rounded-lg bg-rose-500/10 p-3 text-sm text-rose-700 dark:text-rose-300">
              octopaas could not be run: {state.cliError}. Check its path in Settings.
            </p>
          )}
          {failure && (
            <div
              role="alert"
              className="flex gap-2 rounded-md border border-amber-500/25 bg-amber-500/5 px-3 py-2.5 text-xs text-zinc-700 dark:text-zinc-300"
            >
              <TriangleAlert size={14} className="mt-px shrink-0 text-amber-600 dark:text-amber-500" aria-hidden />
              <span>{failure}</span>
            </div>
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
