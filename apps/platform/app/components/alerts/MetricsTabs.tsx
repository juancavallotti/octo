"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Activity, BellRing } from "lucide-react";

/**
 * The two views under Metrics: the deployments themselves, and the watches over
 * them.
 *
 * A link bar rather than the shared TabStrip, because these are routes and not
 * panels — each has its own URL, its own data and its own loading state, and a
 * tablist that swapped a panel would put both monitors in one component's memory
 * for the sake of a keyboard model neither needs.
 *
 * Alerts sit here rather than in their own top-level section because a watch is
 * a standing question about exactly what this section already shows. Somebody
 * looking at a deployment's error rate and somebody setting an alert on it have
 * come for the same thing.
 */

const TABS = [
  { href: "/platform/metrics", label: "Deployments", icon: Activity },
  { href: "/platform/metrics/alerts", label: "Alerts", icon: BellRing },
] as const;

export default function MetricsTabs() {
  const pathname = usePathname();

  return (
    <nav
      aria-label="Metrics views"
      className="flex items-center gap-1 border-b border-black/10 px-6 dark:border-white/10"
    >
      {TABS.map(({ href, label, icon: Icon }) => {
        // The deployments tab is the section root, so it only claims an exact
        // match; anything under /alerts belongs to the alerts tab, including a
        // single watch's page.
        const active =
          href === "/platform/metrics"
            ? pathname === href
            : pathname.startsWith(href);
        return (
          <Link
            key={href}
            href={href}
            aria-current={active ? "page" : undefined}
            className={`-mb-px flex items-center gap-1.5 border-b-2 px-3 py-2 text-sm ${
              active
                ? "border-zinc-900 font-medium text-zinc-900 dark:border-zinc-100 dark:text-zinc-100"
                : "border-transparent text-zinc-500 hover:text-zinc-800 dark:text-zinc-400 dark:hover:text-zinc-200"
            }`}
          >
            <Icon size={14} />
            {label}
          </Link>
        );
      })}
    </nav>
  );
}
