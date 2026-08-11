"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { ArrowLeft, Bot, Mail, Settings, Sparkles } from "lucide-react";

/**
 * The admin section's own switcher, in the shared header's controls slot.
 *
 * Modelled on ManagementNav, and here for the same reason: without it every
 * settings page was a dead end, and moving from Email to LLM provider meant going
 * back to the hub first. The section is small enough that the whole of it fits in
 * one segmented control, so there is no reason to make anyone navigate up.
 *
 * The tiles on the hub still exist — they describe what each page is for, which a
 * one-word tab cannot.
 */

export interface AdminSection {
  key: string;
  href: string;
  label: string;
  icon: typeof Mail;
}

/**
 * Every admin page, in the order the hub lists them. Exported so the hub's tiles
 * and this bar cannot disagree about what exists.
 */
export const ADMIN_SECTIONS: readonly AdminSection[] = [
  { key: "overview", href: "/platform/admin", label: "Overview", icon: Settings },
  { key: "email", href: "/platform/admin/email", label: "Email", icon: Mail },
  { key: "llm", href: "/platform/admin/llm", label: "LLM provider", icon: Sparkles },
  { key: "agent", href: "/platform/admin/agent", label: "Platform agent", icon: Bot },
] as const;

export default function AdminNav({ sections }: { sections?: readonly AdminSection[] }) {
  const pathname = usePathname();
  const items = sections ?? ADMIN_SECTIONS;

  // The longest matching href wins, so Overview ("/platform/admin") is not lit on
  // every page beneath it — the same rule the section bar uses.
  const activeHref = items.reduce<string | null>((best, s) => {
    const match = pathname === s.href || pathname.startsWith(`${s.href}/`);
    if (!match) return best;
    return best === null || s.href.length > best.length ? s.href : best;
  }, null);

  return (
    <div className="flex min-w-0 items-center gap-3">
      {/* Back to the platform proper. The admin section replaces the section bar
          rather than sitting inside it, so without this the way out is the logo,
          which does not read as one. */}
      <Link
        href="/platform"
        title="Back to the platform"
        className="flex shrink-0 items-center gap-1 rounded-md px-1.5 py-1 text-sm text-zinc-500 transition-colors hover:bg-black/[0.04] hover:text-zinc-800 dark:hover:bg-white/[0.06] dark:hover:text-zinc-200"
      >
        <ArrowLeft size={14} />
        <span className="hidden sm:inline">Platform</span>
      </Link>

      <nav className="flex items-center gap-0.5 rounded-md bg-black/[0.04] p-0.5 dark:bg-white/[0.06]">
        {items.map((s) => {
          const active = s.href === activeHref;
          const Icon = s.icon;
          return (
            <Link
              key={s.key}
              href={s.href}
              aria-current={active ? "page" : undefined}
              className={`flex items-center gap-1.5 rounded px-2.5 py-1 text-sm font-medium transition-colors ${
                active
                  ? "bg-white text-zinc-900 shadow-sm dark:bg-zinc-700 dark:text-white"
                  : "text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200"
              }`}
            >
              <Icon size={14} className="shrink-0" />
              <span className="hidden sm:inline">{s.label}</span>
            </Link>
          );
        })}
      </nav>
    </div>
  );
}
