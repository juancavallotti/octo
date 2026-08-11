import AppHeader from "@/app/components/AppHeader";
import UserMenu from "@/app/components/UserMenu";
import { ConfirmProvider } from "@/app/components/ConfirmDialog";
import EmailSettingsManager from "@/app/components/admin/EmailSettingsManager";

/**
 * Email settings (`/platform/admin/email`). ConfirmProvider is here because
 * removing a stored API key asks first.
 */
export default function AdminEmailPage() {
  return (
    <div className="flex h-full flex-col">
      <AppHeader userMenu={<UserMenu />} />
      <div className="min-h-0 flex-1">
        <ConfirmProvider>
          <EmailSettingsManager />
        </ConfirmProvider>
      </div>
    </div>
  );
}
