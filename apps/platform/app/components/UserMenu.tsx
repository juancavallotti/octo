import { auth, authEnabled, signOut } from "@/auth";
import UserMenuClient from "./UserMenuClient";

/**
 * Top-bar account control. Renders nothing when SSO is disabled (local `task dev`)
 * or no session is present, so the header is unchanged in those cases. Fetches the
 * session on the server and hands the user's profile (name, email, picture) plus a
 * server-action sign-out to the client dropdown.
 */
export default async function UserMenu() {
  if (!authEnabled) return null;
  const session = await auth();
  if (!session?.user) return null;

  async function signOutAction() {
    "use server";
    await signOut({ redirectTo: "/" });
  }

  return (
    <UserMenuClient
      name={session.user.name ?? null}
      email={session.user.email ?? null}
      image={session.user.image ?? null}
      signOutAction={signOutAction}
    />
  );
}
