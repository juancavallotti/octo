import { redirect } from "next/navigation";
import { auth, authEnabled } from "@/auth";
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
 */
export default async function SessionLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  let userKey = "local";
  if (authEnabled) {
    const session = await auth();
    if (!session?.user) redirect("/");
    // Only ever a sessionStorage key for the conversation id, so that it cannot be
    // resumed by whoever signs in next on a shared machine. The identity the agent
    // actually trusts is read server-side by the chat route.
    userKey = session.user.id ?? session.user.email ?? "user";
  }
  return <AgentChatLauncher userKey={userKey}>{children}</AgentChatLauncher>;
}
