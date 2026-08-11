import { ConfirmProvider } from "@/app/components/ConfirmDialog";
import AgentSettingsManager from "@/app/components/admin/AgentSettingsManager";

/**
 * The platform agent (`/platform/admin/agent`). ConfirmProvider is here because
 * removing the agent and rolling an update over local edits both ask first.
 */
export default function AdminAgentPage() {
  return (
    <ConfirmProvider>
      <AgentSettingsManager />
    </ConfirmProvider>
  );
}
