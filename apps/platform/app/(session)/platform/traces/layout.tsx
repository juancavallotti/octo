import type { ReactNode } from "react";
import UserMenu from "@/app/components/UserMenu";
import TracesManager from "@/app/components/traces/TracesManager";

/**
 * Layout for the traces routes. The manager lives here rather than in the page so
 * that navigating between apps and traces — which changes the [[...path]] segment
 * — preserves it instead of remounting and refetching. Opening a trace should not
 * reload the list it was opened from.
 *
 * The selection lives in the URL; the manager reads it with usePathname and
 * changes it with the router, so a trace is bookmarkable and shareable, which is
 * most of what anyone wants to do with one. The page underneath is a no-op marker
 * that makes the dynamic route resolvable.
 */
export default function TracesLayout({ children }: { children: ReactNode }) {
  return (
    <>
      <TracesManager userMenu={<UserMenu />} />
      {children}
    </>
  );
}
