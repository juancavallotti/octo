import AppHeader from "@/app/components/AppHeader";
import UserMenu from "@/app/components/UserMenu";
import AdminTiles from "./AdminTiles";

/**
 * The admin hub (`/platform/admin`): site-wide settings, reached from the user
 * menu. Renders the shared header without ManagementNav, matching the account
 * route — this section sits beside the account rather than in the section bar.
 */
export default function AdminPage() {
  return (
    <div className="flex h-full flex-col">
      <AppHeader userMenu={<UserMenu />} />
      <div className="min-h-0 flex-1">
        <AdminTiles />
      </div>
    </div>
  );
}
