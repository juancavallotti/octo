import { ConfirmProvider } from "@/app/components/ConfirmDialog";
import LlmSettingsManager from "@/app/components/admin/LlmSettingsManager";

/**
 * LLM provider settings (`/platform/admin/llm`). ConfirmProvider is here because
 * removing a stored API key asks first.
 */
export default function AdminLlmPage() {
  return (
    <ConfirmProvider>
      <LlmSettingsManager />
    </ConfirmProvider>
  );
}
