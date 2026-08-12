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
 * this layout only owns the full-height shell — and the agent's launcher, which is
 * here because it belongs on every signed-in page and this is the one shell they
 * all share. It renders nothing when the agent is not deployed.
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
  return (
    <div className="flex h-full flex-1 flex-col">
      {children}
      <AgentChatLauncher userKey={userKey} />
    </div>
  );
}
