import { redirect } from "next/navigation";
import AppHeader from "@/app/components/AppHeader";
import UserMenu from "@/app/components/UserMenu";
import { ForbiddenError, requireRole } from "@/app/auth/guard";
import { PLATFORM_ADMIN } from "@/app/auth/roles";
import AdminNav from "./AdminNav";

/**
 * The shell every admin page shares: the header, the section switcher, and the
 * full-height frame.
 *
 * A layout rather than a component each page mounts, so the bar cannot go missing
 * from a page somebody adds later — which is what happened to the settings pages
 * before it existed. Each page below is now just its manager.
 *
 * The same argument is why the administrator check is here. It covers every page
 * in the section, including the next one somebody adds. It is not, however, what
 * protects the section: each action underneath is a POST endpoint of its own and
 * carries its own `withAdmin`. This is so somebody without the role is sent
 * somewhere useful instead of watching a page fail request by request.
 */
export default async function AdminLayout({ children }: { children: React.ReactNode }) {
  try {
    await requireRole(PLATFORM_ADMIN);
  } catch (err) {
    if (!(err instanceof ForbiddenError)) throw err;
    // Back to the dashboard rather than a refusal page. They are signed in and
    // there is somewhere for them to be; a dead end would only need a link back.
    redirect("/platform");
  }
  return (
    <div className="flex h-full flex-col">
      <AppHeader userMenu={<UserMenu />}>
        <AdminNav />
      </AppHeader>
      <div className="min-h-0 flex-1">{children}</div>
    </div>
  );
}
