"use client";

import Image from "next/image";
import { Server, Settings2 } from "lucide-react";
import { WorkspacePicker } from "@octo/editor";
import type { PlatformState, PlatformTab } from "../types";
import IdentityDot from "./IdentityDot";

const TABS: { id: PlatformTab; label: string }[] = [
  { id: "integrations", label: "Integrations" },
  { id: "folder", label: "This folder" },
  { id: "deployments", label: "Deployments" },
  { id: "logs", label: "Logs" },
  { id: "settings", label: "Installations" },
];

/**
 * The window's top bar, the editor header's twin: the mark, the installation chip
 * where the editor has its folder chip, and the tabs centred where the editor has its
 * views.
 */
export default function PlatformHeader({
  state,
  profile,
  onProfile,
  tab,
  onTab,
}: {
  state: PlatformState | null;
  profile: string | null;
  onProfile(name: string): void;
  tab: PlatformTab;
  onTab(tab: PlatformTab): void;
}) {
  const current = state?.profiles.find((p) => p.name === profile);
  return (
    <header className="flex h-12 shrink-0 items-center gap-2 border-b border-black/10 px-4 dark:border-white/10">
      <div className="flex min-w-0 flex-1 basis-0 items-center gap-2">
        <Image src="/octo-logo.png" alt="Octo logo" width={24} height={24} className="h-6 w-auto shrink-0" priority />
        <span className="shrink-0 font-semibold tracking-tight">Platform</span>
        <span className="mx-1 h-5 w-px shrink-0 bg-black/10 dark:bg-white/10" />
        {state && state.profiles.length > 0 && (
          <WorkspacePicker
            label="Installation"
            icon={current ? <IdentityDot identity={current.identity} /> : <Server size={14} className="text-zinc-400" />}
            current={profile ?? "No installation"}
            currentHint={current?.url}
            items={state.profiles
              .filter((p) => p.name !== profile)
              .map((p) => ({ id: p.name, name: p.name, hint: p.url, badge: <IdentityDot identity={p.identity} /> }))}
            onSelect={onProfile}
            actions={[
              {
                label: "Manage installations…",
                icon: <Settings2 size={16} className="shrink-0 text-zinc-400" />,
                onClick: () => onTab("settings"),
              },
            ]}
            searchPlaceholder="Search installations…"
            emptyLabel="No other installations"
          />
        )}
      </div>
      <nav className="flex items-center gap-1 rounded-lg bg-black/[0.04] p-0.5 dark:bg-white/[0.06]">
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            aria-pressed={tab === t.id}
            onClick={() => onTab(t.id)}
            className={`rounded-md px-2.5 py-1 text-[13px] transition-colors ${
              tab === t.id
                ? "bg-white text-zinc-900 shadow-sm dark:bg-zinc-800 dark:text-zinc-100"
                : "text-zinc-600 hover:text-zinc-900 dark:text-zinc-400 dark:hover:text-zinc-100"
            }`}
          >
            {t.label}
          </button>
        ))}
      </nav>
      <div className="flex-1 basis-0" />
    </header>
  );
}
