"use client";

import Link from "next/link";
import type { ReactNode } from "react";
import AppHeader from "@/app/components/AppHeader";
import ManagementNav from "@/app/components/ManagementNav";

/**
 * What the integrations view is when there is no orchestrator to talk to.
 *
 * It keeps the header, so the nav still works and the page does not look broken
 * — this is a configuration state, not an error.
 */
export default function IntegrationsUnavailable({
  userMenu,
}: {
  userMenu?: ReactNode;
}) {
  return (
    <div className="flex h-full flex-col">
      <AppHeader userMenu={userMenu}>
        <ManagementNav />
      </AppHeader>
      <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
        <p className="text-sm text-zinc-500">
          Integration management is unavailable. Set{" "}
          <code className="rounded bg-black/[0.06] px-1 dark:bg-white/10">
            ORCHESTRATOR_URL
          </code>{" "}
          to enable it.
        </p>
        <Link href="/platform" className="text-sm text-sky-600 hover:underline">
          Back to dashboard
        </Link>
      </div>
    </div>
  );
}
