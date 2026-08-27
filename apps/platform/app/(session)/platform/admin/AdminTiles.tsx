"use client";

import { Activity, Bot, Mail, Trash2 } from "lucide-react";
import { ShortcutTile } from "../DashboardTiles";

/**
 * The admin hub's grid of site-wide capabilities.
 *
 * A client component because it owns the icons: lucide-react ships no "use client"
 * directive, so its components are plain functions and a server page cannot pass one
 * across the RSC boundary. Dashboard.tsx does the same for the same reason.
 *
 * Everything listed here is built. The grid carried a "Coming soon" tile for as
 * long as data retention was the one capability the section described but did not
 * have; now that it exists, the list is simply what the section does.
 */

export const LIVE = [
  {
    href: "/platform/admin/email",
    icon: Mail,
    title: "Email",
    subtitle: "Provider key and the address notifications come from",
  },
  {
    href: "/platform/admin/agent",
    icon: Bot,
    title: "Platform agent",
    subtitle:
      "The LLM and embedding providers Dr. Octo runs on, and his own deployment",
  },
  {
    href: "/platform/admin/retention",
    icon: Trash2,
    title: "Data retention",
    subtitle: "How long stored logs and traces are kept",
  },
  {
    href: "/platform/admin/health",
    icon: Activity,
    title: "Platform services",
    subtitle: "Whether Postgres, Redis, NATS and the cluster are reachable",
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
        </div>
      </div>
    </div>
  );
}
