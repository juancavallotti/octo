import AppHeader from "@/app/components/AppHeader";
import UserMenu from "@/app/components/UserMenu";
import { ConfirmProvider } from "@/app/components/ConfirmDialog";
import AgentSettingsManager from "@/app/components/admin/AgentSettingsManager";

/**
 * The platform agent (`/platform/admin/agent`). ConfirmProvider is here because
 * removing the agent and rolling an update over local edits both ask first.
 */
export default function AdminAgentPage() {
  return (
    <div className="flex h-full flex-col">
      <AppHeader userMenu={<UserMenu />} />
      <div className="min-h-0 flex-1">
        <ConfirmProvider>
          <AgentSettingsManager />
        </ConfirmProvider>
      </div>
    </div>
  );
}
