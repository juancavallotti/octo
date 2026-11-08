import { ConfirmProvider } from "@/app/components/ConfirmDialog";
import EnvironmentsManager from "@/app/components/admin/environments/EnvironmentsManager";

/**
 * Environments (`/platform/admin/environments`). ConfirmProvider is here because
 * deleting an environment deletes its namespace, and asks first.
 */
export default function AdminEnvironmentsPage() {
  return (
    <ConfirmProvider>
      <EnvironmentsManager />
    </ConfirmProvider>
  );
}
