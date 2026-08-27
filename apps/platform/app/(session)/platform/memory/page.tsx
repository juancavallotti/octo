import AppHeader from "@/app/components/AppHeader";
import ManagementNav from "@/app/components/ManagementNav";
import UserMenu from "@/app/components/UserMenu";
import { ConfirmProvider } from "@/app/components/ConfirmDialog";
import AgentMemoryManager from "@/app/components/memory/AgentMemoryManager";

/**
 * The agent memory route (`/platform/memory`): what agents have recorded, by
 * integration and by agent, behind the shared header with the section nav.
 *
 * It sat under Admin, which was wrong in the way the Object Store would be wrong
 * there: admin is settings that belong to the installation, and this is *data* that
 * belongs to integrations — read the same way logs, traces and stored objects are.
 * Nothing on this page configures anything.
 *
 * ConfirmProvider is here because everything destructive on it asks first: erasing
 * a conversation and forgetting a fact are both irreversible.
 */
export default function MemoryPage() {
  return (
    <div className="flex h-full flex-col">
      <AppHeader userMenu={<UserMenu />}>
        <ManagementNav />
      </AppHeader>
      <ConfirmProvider>
        <div className="min-h-0 flex-1">
          <AgentMemoryManager />
        </div>
      </ConfirmProvider>
    </div>
  );
}
