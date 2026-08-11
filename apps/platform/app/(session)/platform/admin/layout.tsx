import AppHeader from "@/app/components/AppHeader";
import UserMenu from "@/app/components/UserMenu";
import AdminNav from "./AdminNav";

/**
 * The shell every admin page shares: the header, the section switcher, and the
 * full-height frame.
 *
 * A layout rather than a component each page mounts, so the bar cannot go missing
 * from a page somebody adds later — which is what happened to the settings pages
 * before it existed. Each page below is now just its manager.
 */
export default function AdminLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-full flex-col">
      <AppHeader userMenu={<UserMenu />}>
        <AdminNav />
      </AppHeader>
      <div className="min-h-0 flex-1">{children}</div>
    </div>
  );
}
