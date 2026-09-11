import { auth } from "@/auth";
import UsersManager from "@/app/components/admin/users/UsersManager";

/**
 * Who may use this platform.
 *
 * The section's layout already requires the administrator role. What this page
 * adds is knowing *which* administrator is looking, so the screen can refuse to
 * let somebody edit their own roles and lock themselves out of it.
 *
 * Without that id the guard would pass for every row, so the list is not offered
 * at all rather than offered unguarded.
 */
export default async function UsersPage() {
  const session = await auth();
  const currentUserId = session?.user?.id;
  if (!currentUserId) {
    return (
      <p className="p-6 text-sm text-zinc-500">
        Your session does not carry a platform user id, so this page cannot tell
        which row is yours. Sign out and in again.
      </p>
    );
  }
  return <UsersManager currentUserId={currentUserId} />;
}
