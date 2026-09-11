import { redirect } from "next/navigation";
import { auth } from "@/auth";
import { RolesProvider } from "@/app/auth/RolesContext";
import { writeRoles } from "@/app/auth/guard";
import AgentChatLauncher from "@/app/components/agent/AgentChatLauncher";

/**
 * Render every signed-in route per request. The account tile (UserMenu) reads the
 * session via `auth()`, so a statically prerendered page would bake in the
 * signed-out placeholder — the cause of the blank account circle on routes that
 * touch no other dynamic API (the dashboard, `/platform/new`). Forcing it here, at
 * the shared session boundary, covers them all rather than per page.
 */
export const dynamic = "force-dynamic";

/**
 * Layout for the signed-in platform. Requires a session, redirecting to the
 * welcome page without one, and puts the caller's roles into the client tree.
 *
 * It delegates the full-height shell to the agent's launcher, which owns it
 * because the chat panel can be pinned — docked, the page has to shrink into the
 * space beside it.
 */
export default async function SessionLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const session = await auth();
  if (!session?.user) redirect("/");
  // Only ever a sessionStorage key for the conversation id, so that it cannot be
  // resumed by whoever signs in next on a shared machine. The identity the agent
  // actually trusts is read server-side by the chat route.
  const userKey = session.user.id ?? session.user.email ?? "user";
  const roles = session.user.roles ?? [];
  return (
    <RolesProvider
      roles={roles}
      mayWrite={roles.some((role) => writeRoles.includes(role))}
    >
      <AgentChatLauncher userKey={userKey}>{children}</AgentChatLauncher>
    </RolesProvider>
  );
}
