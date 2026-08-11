"use client";

import { Bot, Mail, Sparkles, Trash2 } from "lucide-react";
import { ShortcutTile } from "../DashboardTiles";

/**
 * The admin hub's grid of site-wide capabilities.
 *
 * A client component because it owns the icons: lucide-react ships no "use client"
 * directive, so its components are plain functions and a server page cannot pass one
 * across the RSC boundary. Dashboard.tsx does the same for the same reason.
 *
 * The unbuilt capabilities are listed rather than hidden, so the section reads as
 * intentional rather than half-finished, and so the roadmap is visible where someone
 * would go looking for it.
 */

const LIVE = [
  {
    href: "/platform/admin/email",
    icon: Mail,
    title: "Email",
    subtitle: "Provider key and the address notifications come from",
  },
  {
    href: "/platform/admin/llm",
    icon: Sparkles,
    title: "LLM provider",
    subtitle: "Provider, model and API key for the platform agent",
  },
  {
    href: "/platform/admin/agent",
    icon: Bot,
    title: "Platform agent",
    subtitle: "Install Dr. Octo, roll out updates, and trace him",
  },
] as const;

const COMING_SOON = [
  {
    icon: Trash2,
    title: "Data retention",
    subtitle: "Scheduled cleanup of old logs and traces",
  },
] as const;

export default function AdminTiles() {
  return (
    <div className="flex h-full flex-col overflow-y-auto px-6 py-5">
      <div className="mx-auto w-full max-w-2xl">
        <h1 className="text-lg font-semibold">Admin</h1>
        <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
          Settings that belong to this installation rather than to any one
          integration.
        </p>

        <div className="mt-5 grid gap-3 sm:grid-cols-2">
          {LIVE.map((tile) => (
            <ShortcutTile key={tile.title} {...tile} />
          ))}
          {COMING_SOON.map((tile) => (
            <ShortcutTile key={tile.title} {...tile} href="" disabled badge="Coming soon" />
          ))}
        </div>
      </div>
    </div>
  );
}
