"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { MANAGEMENT_SECTIONS } from "@/app/components/integrations/views";

/**
 * The platform's section switcher: a segmented control of links across the
 * top-level routes (Dashboard, Integrations, Deployments, …), highlighting the one
 * matching the current path. Rendered in the shared AppHeader on every signed-in
 * page so the bar stays put as you move between sections.
 */
export default function ManagementNav() {
  const pathname = usePathname();
  // Highlight the single best (longest) matching section, so the Dashboard tab
  // ("/platform") isn't lit on every subroute it prefixes.
  const activeHref = MANAGEMENT_SECTIONS.reduce<string | null>((best, s) => {
    const match = pathname === s.href || pathname.startsWith(`${s.href}/`);
    if (!match) return best;
    return best === null || s.href.length > best.length ? s.href : best;
  }, null);
  return (
    <nav className="flex shrink-0 items-center gap-0.5 rounded-md bg-black/[0.04] p-0.5 dark:bg-white/[0.06]">
      {MANAGEMENT_SECTIONS.map((s) => {
        const active = s.href === activeHref;
        const Icon = s.icon;
        return (
          <Link
            key={s.key}
            href={s.href}
            aria-current={active ? "page" : undefined}
            aria-label={s.label}
            title={s.label}
            className={`flex items-center gap-1.5 rounded px-2.5 py-1 text-sm font-medium whitespace-nowrap transition-colors ${
              active
                ? "bg-white text-zinc-900 shadow-sm dark:bg-zinc-700 dark:text-white"
                : "text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200"
            }`}
          >
            <Icon size={14} className="shrink-0" />
            <span className="@max-6xl:hidden">{s.label}</span>
          </Link>
        );
      })}
    </nav>
  );
}
