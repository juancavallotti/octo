import { ConfirmProvider } from "@/app/components/ConfirmDialog";
import AgentMemoryManager from "@/app/components/memory/AgentMemoryManager";

/**
 * Agent memory viewer (`/platform/admin/memory`). ConfirmProvider is here because
 * everything destructive on this page asks first — erasing a conversation and
 * forgetting a fact are both irreversible.
 */
export default function AdminMemoryPage() {
  return (
    <ConfirmProvider>
      <AgentMemoryManager />
    </ConfirmProvider>
  );
}
