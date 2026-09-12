import { redirect } from "next/navigation";
import AppHeader from "@/app/components/AppHeader";
import ManagementNav from "@/app/components/ManagementNav";
import UserMenu from "@/app/components/UserMenu";
import { ConfirmProvider } from "@/app/components/ConfirmDialog";
import SecretsManager from "@/app/components/integrations/SecretsManager";
import { ForbiddenError, requireRole } from "@/app/auth/guard";
import { PLATFORM_ADMIN } from "@/app/auth/roles";

/**
 * The secrets management route (`/platform/secrets`): the cluster-wide secret
 * catalog, behind the shared header with the management section nav. A server
 * component so it can hand the header the server-rendered account tile, matching
 * the sibling integrations route.
 *
 * Administrators only, reads included: these are the installation's own
 * credentials, and the list of their names says nearly as much as their values.
 * This is not what protects them — every action underneath carries its own gate,
 * and so does the orchestrator — it is so somebody without the role is sent
 * somewhere useful instead of watching a page fail request by request.
 */
export default async function SecretsPage() {
  try {
    await requireRole(PLATFORM_ADMIN);
  } catch (err) {
    if (!(err instanceof ForbiddenError)) throw err;
    redirect("/platform");
  }
  return (
    <div className="flex h-full flex-col">
      <AppHeader userMenu={<UserMenu />}>
        <ManagementNav />
      </AppHeader>
      <div className="min-h-0 flex-1">
        <ConfirmProvider>
          <SecretsManager />
        </ConfirmProvider>
      </div>
    </div>
  );
}
