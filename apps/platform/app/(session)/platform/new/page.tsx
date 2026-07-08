import { probeSchema } from "@octo/run-host";
import type { Capabilities } from "@octo/editor";
import PlatformEditor from "@/app/components/PlatformEditor";
import UserMenu from "@/app/components/UserMenu";

/**
 * The "new integration" route (`/platform/new`): opens the editor on a fresh,
 * unsaved document. On first save the editor promotes the URL to the
 * bookmarkable `/platform/i/<id>` form (see PlatformEditor.onSaved).
 *
 * The capability schema is probed from the `octo` binary and injected into the
 * client editor (null when no runner is configured -> empty bundled fallback).
 */
export default async function NewIntegrationPage() {
  const capabilities = (await probeSchema()) as Capabilities | null;
  return (
    <PlatformEditor userMenu={<UserMenu />} capabilities={capabilities} />
  );
}
