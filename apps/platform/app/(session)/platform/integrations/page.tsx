import UserMenu from "@/app/components/UserMenu";
import { ConfirmProvider } from "@/app/components/ConfirmDialog";
import IntegrationsManager from "@/app/components/integrations/IntegrationsManager";
import {
  MANAGEMENT_VIEWS,
  type ManagementView,
} from "@/app/components/integrations/views";

/**
 * The integration management route (`/platform/integrations`): the file manager,
 * deployments, and secrets, behind the shared header. A server component so it can
 * hand the client manager the server-rendered account tile; `?view=secrets` opens
 * straight on the secrets tab, and `?integration=<id>` preselects an integration
 * (both used by the dashboard shortcuts/tiles).
 */
export default async function IntegrationsPage({
  searchParams,
}: {
  searchParams: Promise<{ view?: string; integration?: string }>;
}) {
  const { view, integration } = await searchParams;
  const initialView = MANAGEMENT_VIEWS.includes(view as ManagementView)
    ? (view as ManagementView)
    : "integrations";
  return (
    <ConfirmProvider>
      <IntegrationsManager
        initialView={initialView}
        initialSelectedId={integration ?? null}
        userMenu={<UserMenu />}
      />
    </ConfirmProvider>
  );
}
