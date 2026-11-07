"use client";

import { useEffect } from "react";
import { usePathname, useSearchParams } from "next/navigation";
import { useEnvironment } from "@/app/auth/EnvironmentContext";
import { ENVIRONMENT_PARAM } from "@/app/lib/environment";

/**
 * Keeps the address bar saying which environment the page is in, so any page can
 * be bookmarked or shared and opens there again: `?env=<name>` outside the default,
 * nothing in it. It rewrites the address in place and never navigates.
 *
 * The admin section belongs to no environment, so its addresses carry none.
 */
export default function EnvironmentInUrl() {
  const context = useEnvironment();
  const pathname = usePathname();
  const params = useSearchParams();
  const environment = context?.environment ?? "";

  useEffect(() => {
    if (!context) return;
    const want = pathname.startsWith("/platform/admin") ? "" : environment;
    const shown = params.get(ENVIRONMENT_PARAM);
    // Already right: the environment named, or nothing named in the default.
    if (want === "" ? shown === null : shown === want) return;
    const url = new URL(window.location.href);
    if (want === "") url.searchParams.delete(ENVIRONMENT_PARAM);
    else url.searchParams.set(ENVIRONMENT_PARAM, want);
    window.history.replaceState(window.history.state, "", url);
  }, [context, environment, pathname, params]);

  return null;
}
