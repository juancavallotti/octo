"use client";

import { useMemo, useState } from "react";
import { RefreshCw, Search } from "lucide-react";
import type { PlatformState } from "../types";
import { useIntegrations } from "../useIntegrations";
import { CARD, INPUT, MUTED, SECONDARY, SECTION_TITLE } from "../ui";
import ErrorLine from "./ErrorLine";
import IntegrationRow from "./IntegrationRow";
import SignInPrompt from "./SignInPrompt";

/**
 * The installation's integrations, each one a pull away from being the open folder —
 * or, when this machine already has it, an open away.
 */
export default function IntegrationsTab({
  state,
  profile,
  onManage,
}: {
  state: PlatformState;
  profile: string | null;
  onManage(): void;
}) {
  const installation = state.profiles.find((p) => p.name === profile);
  const signedIn = installation?.identity.state === "signed-in";
  const { items, error, reload } = useIntegrations(profile, signedIn);
  const [query, setQuery] = useState("");

  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    return (items ?? []).filter((i) => !q || i.name.toLowerCase().includes(q));
  }, [items, query]);

  if (!installation) {
    return (
      <div className={`${CARD} flex flex-col items-start gap-3 p-6`}>
        <p className="text-sm">Add the platform you deploy to, to pull its integrations into a folder here.</p>
        <button type="button" onClick={onManage} className={SECONDARY}>
          Add an installation…
        </button>
      </div>
    );
  }
  if (!signedIn) return <SignInPrompt installation={installation} signingIn={state.signingIn} />;

  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <h2 className={SECTION_TITLE}>Integrations on {installation.name}</h2>
        <span className="flex-1" />
        <label className="flex items-center gap-1.5">
          <Search size={14} className="text-zinc-400" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search integrations…"
            aria-label="Search integrations"
            className={INPUT}
          />
        </label>
        <button type="button" onClick={reload} className={SECONDARY} title="Reload">
          <RefreshCw size={14} />
        </button>
      </div>
      <ErrorLine error={error} />
      {items === null && !error && <p className={MUTED}>Loading…</p>}
      {items !== null && shown.length === 0 && (
        <p className={MUTED}>{query ? "No matches" : "No integrations here yet. Put this folder on the platform from This folder."}</p>
      )}
      {shown.length > 0 && (
        <ul className={`${CARD} divide-y divide-black/5 dark:divide-white/5`}>
          {shown.map((i) => (
            <IntegrationRow key={i.id} profile={installation.name} integration={i} openPath={state.vault?.path ?? null} />
          ))}
        </ul>
      )}
      <p className={MUTED}>
        Pulled integrations land in {state.projectsDir}. You can choose another folder each time.
      </p>
    </section>
  );
}
