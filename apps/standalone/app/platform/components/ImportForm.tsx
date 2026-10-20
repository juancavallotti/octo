"use client";

import { useState } from "react";
import { CloudUpload } from "lucide-react";
import { paasBridge } from "../../desktop";
import type { PlatformState } from "../types";
import { useAction } from "../useAction";
import { CARD, INPUT, MUTED, PRIMARY } from "../ui";
import ErrorLine from "./ErrorLine";

/** Put a folder that is on no platform yet on one, as a new integration. */
export default function ImportForm({ state, profile }: { state: PlatformState; profile: string | null }) {
  const paas = paasBridge();
  const { busy, error, run } = useAction();
  const [target, setTarget] = useState(profile ?? "");
  const [name, setName] = useState(state.vault?.name ?? "");
  const chosen = state.profiles.some((p) => p.name === target) ? target : (profile ?? "");

  if (state.profiles.length === 0) {
    return <p className={MUTED}>Add an installation under Installations to put this folder on a platform.</p>;
  }

  return (
    <section className={`${CARD} flex flex-col gap-3 p-4`}>
      <p className="text-sm">
        <span className="font-medium">{state.vault?.name}</span> is not on a platform yet. Import it as a new
        integration, and this folder becomes it: push, version and deploy it from here.
      </p>
      <div className="flex flex-wrap items-center gap-2">
        <select value={chosen} onChange={(e) => setTarget(e.target.value)} aria-label="Installation" className={INPUT}>
          {state.profiles.map((p) => (
            <option key={p.name} value={p.name}>
              {p.name} — {p.url}
            </option>
          ))}
        </select>
        <input value={name} onChange={(e) => setName(e.target.value)} aria-label="Integration name" placeholder="Name" className={`${INPUT} flex-1`} />
        <button
          type="button"
          disabled={busy || !chosen || state.activity?.state === "running"}
          onClick={() => paas && void run(() => paas.importFolder(chosen, name))}
          className={PRIMARY}
        >
          <CloudUpload size={14} /> Import
        </button>
      </div>
      <ErrorLine error={error} />
    </section>
  );
}
