import { redirect } from "next/navigation";
import { auth, authEnabled } from "@/auth";
import { RolesProvider } from "@/app/auth/RolesContext";
import { ALL_ROLES } from "@/app/auth/roles";
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
 * Layout for the signed-in platform (dashboard, editor, file manager). The proxy
 * middleware already gates these routes, but we re-check here as defense in depth
 * and to guarantee a session exists for the server-rendered account tile — a
 * missing one bounces to the public welcome page. When SSO is disabled (local
 * dev) the check is skipped and the platform is open.
 *
 * Each page composes its own header from the shared AppLogo + account tile, so
 * this layout only delegates the full-height shell to the agent's launcher, which
 * owns it because the chat panel can be pinned — docked, the page has to shrink
 * into the space beside it. It renders the page alone when the agent is not
 * deployed.
 *
 * It is also where the caller's roles enter the client tree. This is the one
 * place that already has the session, so handing them down from here costs
 * nothing; the provider wraps the launcher rather than sitting inside it, so the
 * launcher itself can be gated on a role later. What that context may and may not
 * be used for is on RolesContext.
 */
export default async function SessionLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  let userKey = "local";
  // With SSO off there is nobody to ask, and the server-side guards pass
  // everything — so the UI is given every role, or it would hide features that
  // local dev can in fact use.
  let roles: string[] = [...ALL_ROLES];
  if (authEnabled) {
    const session = await auth();
    if (!session?.user) redirect("/");
    // Only ever a sessionStorage key for the conversation id, so that it cannot be
    // resumed by whoever signs in next on a shared machine. The identity the agent
    // actually trusts is read server-side by the chat route.
    userKey = session.user.id ?? session.user.email ?? "user";
    roles = session.user.roles ?? [];
  }
  return (
    <RolesProvider roles={roles} enforced={authEnabled}>
      <AgentChatLauncher userKey={userKey}>{children}</AgentChatLauncher>
    </RolesProvider>
  );
}
