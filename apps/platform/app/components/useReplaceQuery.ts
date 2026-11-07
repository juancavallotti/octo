"use client";

import { useCallback } from "react";
import { usePathname, useRouter } from "next/navigation";
import { useEnvironment } from "@/app/auth/EnvironmentContext";
import { withEnvironment } from "@/app/lib/environment";

/**
 * Replaces this page's query string with `query`, in place and without scrolling,
 * for pages whose address mirrors their filters. The environment the page is in
 * stays named in the address, so a page writing only its own filters keeps it.
 */
export function useReplaceQuery(): (query: string) => void {
  const router = useRouter();
  const pathname = usePathname();
  const environment = useEnvironment()?.environment ?? "";
  return useCallback(
    (query: string) => {
      const qs = withEnvironment(query, environment);
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false });
    },
    [router, pathname, environment],
  );
}
