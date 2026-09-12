import { User } from "lucide-react";
import { auth, signOut } from "@/auth";
import UserMenuClient from "./UserMenuClient";

/**
 * Top-bar account control. With a session it renders the full account dropdown;
 * without one it renders a neutral, non-interactive placeholder avatar so the
 * header always carries an account indicator rather than an empty corner. Fetches
 * the session on the server and hands the user's profile (name, email, picture)
 * plus a server-action sign-out to the client dropdown.
 */
export default async function UserMenu() {
  const session = await auth();
  if (!session?.user) return <AccountPlaceholder />;

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

/**
 * The signed-out indicator: a generic avatar circle matching the live avatar's
 * shape and ring, but inert. Communicates "no signed-in user" without leaving the
 * header's account slot empty.
 */
function AccountPlaceholder() {
  const label = "Not signed in";
  return (
    <span
      aria-label={label}
      title={label}
      className="flex h-7 w-7 items-center justify-center rounded-full bg-zinc-200 text-zinc-500 ring-1 ring-black/10 dark:bg-zinc-700 dark:text-zinc-300 dark:ring-white/15"
    >
      <User size={15} />
    </span>
  );
}
