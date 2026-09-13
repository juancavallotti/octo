import AppHeader from "@/app/components/AppHeader";
import ManagementNav from "@/app/components/ManagementNav";
import UserMenu from "@/app/components/UserMenu";
import { ConfirmProvider } from "@/app/components/ConfirmDialog";
import AgentMemoryManager from "@/app/components/memory/AgentMemoryManager";

/**
 * The agent memory route (`/platform/memory`): what agents have recorded, by
 * integration and by agent, behind the shared header with the section nav. This is
 * *data* that belongs to integrations, read the same way logs, traces and stored
 * objects are; nothing on this page configures anything.
 *
 * ConfirmProvider is here because everything destructive on it asks first: erasing
 * a conversation and forgetting a fact are both irreversible.
 *
 * The manager fills the width and the height. What it shows is prose —
 * transcripts, search hits, remembered facts — and a centred column turned every
 * line of somebody's conversation into four wrapped ones.
 */
export default function MemoryPage() {
  return (
    <div className="flex h-full flex-col">
      <AppHeader userMenu={<UserMenu />}>
        <ManagementNav />
      </AppHeader>
      <ConfirmProvider>
        <div className="flex min-h-0 flex-1 flex-col">
          <AgentMemoryManager />
        </div>
      </ConfirmProvider>
    </div>
  );
}
