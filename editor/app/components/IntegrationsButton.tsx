"use client";

import Link from "next/link";
import { LayoutGrid } from "lucide-react";

/** Header button that opens the integration management route. */
export default function IntegrationsButton() {
  return (
    <Link
      href="/integrations"
      title="Manage integrations"
      className="inline-flex items-center gap-1.5 rounded-md border border-black/10 px-3 py-1 text-sm font-medium text-zinc-600 transition-colors hover:bg-black/[0.04] hover:text-zinc-900 dark:border-white/15 dark:text-zinc-300 dark:hover:bg-white/[0.06] dark:hover:text-zinc-100"
    >
      <LayoutGrid className="h-3.5 w-3.5" />
      Integrations
    </Link>
  );
}
