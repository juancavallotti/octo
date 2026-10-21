"use client";

import { FolderOpen, Unlink, Upload } from "lucide-react";
import { paasBridge } from "../../desktop";
import type { PlatformState } from "../types";
import { useAction } from "../useAction";
import { CARD, DANGER, MUTED, PRIMARY, SECTION_TITLE } from "../ui";
import ErrorLine from "./ErrorLine";
import ImportForm from "./ImportForm";
import VersionsSection from "./VersionsSection";

/**
 * The open folder and the platform: which integration it is and on which
 * installation, pushing it there, and its versions — or putting it there when it is
 * not yet.
 */
export default function FolderTab({ state, profile }: { state: PlatformState; profile: string | null }) {
  const paas = paasBridge();
  const { busy, error, run } = useAction();
  const { vault, link } = state;
  const running = state.activity?.state === "running";

  if (!vault) return <p className={MUTED}>No folder is open.</p>;
  if (!link) return <ImportForm state={state} profile={profile} />;

  return (
    <div className="flex flex-col gap-4">
      <section className={`${CARD} flex flex-col gap-3 p-4`}>
        <div className="flex items-center gap-3">
          <FolderOpen size={18} className="shrink-0 text-zinc-400" />
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-medium">{vault.name}</div>
            <div className={`${MUTED} truncate`} title={vault.path}>
              {link.integrationName} on {link.profile} · {link.url}
              {link.pulledTag ? ` · pulled from ${link.pulledTag}` : ""}
            </div>
          </div>
          <button
            type="button"
            disabled={busy || running}
            onClick={() => paas && void run(() => paas.push())}
            title="Save, then replace the integration's draft on the platform with this folder"
            className={PRIMARY}
          >
            <Upload size={14} /> Push
          </button>
          <button type="button" disabled={busy} onClick={() => paas && void run(() => paas.unlink())} className={DANGER}>
            <Unlink size={14} /> Unlink
          </button>
        </div>
        <ErrorLine error={error} />
        <p className={MUTED}>
          Push replaces the integration&apos;s draft with this folder as it is on disk, after saving. Its versions and
          running deployments stay as they are.
        </p>
      </section>
      <section className="flex flex-col gap-2">
        <h2 className={SECTION_TITLE}>Versions</h2>
        <VersionsSection key={`${link.profile}/${link.integrationId}`} busy={running} />
      </section>
    </div>
  );
}
