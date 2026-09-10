import { auth, authEnabled } from "@/auth";
import UsersManager from "@/app/components/admin/users/UsersManager";

/**
 * Who may use this platform. The section's layout already requires the
 * administrator role; this only resolves who is looking, so the page can refuse
 * to let somebody edit their own roles and lock themselves out of it.
 */
export default async function UsersPage() {
  const session = authEnabled ? await auth() : null;
  return <UsersManager currentUserId={session?.user?.id} />;
}
