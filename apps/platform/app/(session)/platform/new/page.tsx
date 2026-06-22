import PlatformEditor from "@/app/components/PlatformEditor";
import UserMenu from "@/app/components/UserMenu";

/**
 * The "new integration" route (`/platform/new`): opens the editor on a fresh,
 * unsaved document. On first save the editor promotes the URL to the
 * bookmarkable `/platform/i/<id>` form (see PlatformEditor.onSaved).
 */
export default function NewIntegrationPage() {
  return <PlatformEditor userMenu={<UserMenu />} />;
}
