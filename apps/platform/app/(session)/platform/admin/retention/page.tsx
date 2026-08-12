import { ConfirmProvider } from "@/app/components/ConfirmDialog";
import RetentionSettingsManager from "@/app/components/admin/RetentionSettingsManager";

/**
 * Data retention settings (`/platform/admin/retention`). ConfirmProvider is here
 * because running a sweep asks first — it is the one action in the admin section
 * that destroys data rather than configuring something.
 */
export default function AdminRetentionPage() {
  return (
    <ConfirmProvider>
      <RetentionSettingsManager />
    </ConfirmProvider>
  );
}
