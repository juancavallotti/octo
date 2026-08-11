import { ConfirmProvider } from "@/app/components/ConfirmDialog";
import EmailSettingsManager from "@/app/components/admin/EmailSettingsManager";

/**
 * Email settings (`/platform/admin/email`). ConfirmProvider is here because
 * removing a stored API key asks first.
 */
export default function AdminEmailPage() {
  return (
    <ConfirmProvider>
      <EmailSettingsManager />
    </ConfirmProvider>
  );
}
