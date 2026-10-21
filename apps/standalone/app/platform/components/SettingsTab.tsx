"use client";

import { FolderOpen } from "lucide-react";
import { paasBridge } from "../../desktop";
import type { PlatformState } from "../types";
import { useAction } from "../useAction";
import { CARD, MUTED, SECONDARY, SECTION_TITLE } from "../ui";
import AddInstallation from "./AddInstallation";
import ErrorLine from "./ErrorLine";
import InstallationRow from "./InstallationRow";

/**
 * The platforms this machine works with — octopaas's profiles, shared with the CLI in
 * the terminal — and where pulled integrations land.
 */
export default function SettingsTab({ state }: { state: PlatformState }) {
  const paas = paasBridge();
  const { busy, error, run } = useAction();

  return (
    <div className="flex flex-col gap-6">
      <section className="flex flex-col gap-2">
        <h2 className={SECTION_TITLE}>Installations</h2>
        {state.profiles.length > 0 && (
          <ul className={`${CARD} divide-y divide-black/5 dark:divide-white/5`}>
            {state.profiles.map((p) => (
              <InstallationRow key={p.name} installation={p} signingIn={state.signingIn} />
            ))}
          </ul>
        )}
        <AddInstallation />
        <p className={MUTED}>
          These are the octopaas CLI&apos;s profiles: the same ones `octopaas profile list` shows in a terminal. The
          default is the one a terminal uses without --profile.
        </p>
      </section>
      <section className="flex flex-col gap-2">
        <h2 className={SECTION_TITLE}>Projects folder</h2>
        <div className={`${CARD} flex items-center gap-3 p-4`}>
          <FolderOpen size={16} className="shrink-0 text-zinc-400" />
          <span className="min-w-0 flex-1 truncate font-mono text-[13px]" title={state.projectsDir}>
            {state.projectsDir}
          </span>
          <button type="button" disabled={busy} onClick={() => paas && void run(() => paas.pickProjectsDir())} className={SECONDARY}>
            Choose…
          </button>
          <button type="button" disabled={busy} onClick={() => paas && void run(() => paas.resetProjectsDir())} className={SECONDARY}>
            Reset
          </button>
        </div>
        <ErrorLine error={error} />
        <p className={MUTED}>Integrations you pull land in a folder named after them here. Each pull lets you choose another place.</p>
      </section>
    </div>
  );
}
