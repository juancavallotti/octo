import { ConfirmProvider } from "@/app/components/ConfirmDialog";
import EmbeddingSettingsManager from "@/app/components/admin/EmbeddingSettingsManager";

/**
 * Embedding settings (`/platform/admin/embedding`). ConfirmProvider is here for
 * the two things on this page that ask first: removing a stored key, and changing
 * the model once anything has been embedded with the old one.
 */
export default function AdminEmbeddingPage() {
  return (
    <ConfirmProvider>
      <EmbeddingSettingsManager />
    </ConfirmProvider>
  );
}
