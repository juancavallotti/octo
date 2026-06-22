import { redirect } from "next/navigation";
import { auth, authEnabled } from "@/auth";

/**
 * Layout for the signed-in platform (dashboard, editor, file manager). The proxy
 * middleware already gates these routes, but we re-check here as defense in depth
 * and to guarantee a session exists for the server-rendered account tile — a
 * missing one bounces to the public welcome page. When SSO is disabled (local
 * dev) the check is skipped and the platform is open.
 *
 * Each page composes its own header from the shared AppLogo + account tile, so
 * this layout only owns the full-height shell.
 */
export default async function SessionLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  if (authEnabled) {
    const session = await auth();
    if (!session?.user) redirect("/");
  }
  return <div className="flex h-full flex-1 flex-col">{children}</div>;
}
