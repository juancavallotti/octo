"use client";

import {
  useFileSystem,
  RunBar,
  IntegrationTitle,
  FolderPicker,
  ViewModeToggle,
  SaveButton,
} from "@octo/editor";
import AppLogo from "./AppLogo";
import IntegrationsButton from "./IntegrationsButton";
import DuplicateButton from "./DuplicateButton";
import TagButton from "./TagButton";
import DeployButton from "./DeployButton";

/**
 * The editor's top bar. The integration controls (title, folder, Tag, Save,
 * manage) only appear when a filesystem capability is present (`useFileSystem()`);
 * otherwise the bar is just the logo and the RUN control.
 */
export default function EditorHeader({
  userMenu,
  getIntegrationId,
}: {
  /** Account control slot (server-rendered UserMenu); only visible when SSO is on. */
  userMenu?: React.ReactNode;
  /** Reads the authoritative integration id (updated on save) for tagging. */
  getIntegrationId: () => string | null;
}) {
  const available = useFileSystem() !== null;

  return (
    <header className="relative flex items-center gap-2 border-b border-black/10 dark:border-white/10 px-4 h-12 shrink-0">
      <AppLogo />

      {available && (
        <>
          <span className="mx-1 h-5 w-px bg-black/10 dark:bg-white/10" />
          <IntegrationTitle />
          <FolderPicker />

          {/* Centred on the bar itself — see StandaloneHeader; both sides of it
              change width with the integration's name and its controls. */}
          <div className="pointer-events-none absolute left-1/2 -translate-x-1/2">
            <div className="pointer-events-auto">
              <ViewModeToggle />
            </div>
          </div>
        </>
      )}

      <div className="ml-auto flex items-center gap-2">
        {available && (
          <>
            <IntegrationsButton getIntegrationId={getIntegrationId} />
            <DuplicateButton getIntegrationId={getIntegrationId} />
            <TagButton getIntegrationId={getIntegrationId} />
            <DeployButton getIntegrationId={getIntegrationId} />
            <SaveButton />
          </>
        )}
        <RunBar />
        {userMenu}
      </div>
    </header>
  );
}
