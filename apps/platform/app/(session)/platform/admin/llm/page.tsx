import AppHeader from "@/app/components/AppHeader";
import UserMenu from "@/app/components/UserMenu";
import { ConfirmProvider } from "@/app/components/ConfirmDialog";
import LlmSettingsManager from "@/app/components/admin/LlmSettingsManager";

/**
 * LLM provider settings (`/platform/admin/llm`). ConfirmProvider is here because
 * removing a stored API key asks first.
 */
export default function AdminLlmPage() {
  return (
    <div className="flex h-full flex-col">
      <AppHeader userMenu={<UserMenu />} />
      <div className="min-h-0 flex-1">
        <ConfirmProvider>
          <LlmSettingsManager />
        </ConfirmProvider>
      </div>
    </div>
  );
}
