"use client";

import {
  useFileSystem,
  RunBar,
  IntegrationTitle,
  FolderPicker,
  SaveButton,
} from "@octo/editor";
import AppLogo from "./AppLogo";
import IntegrationsButton from "./IntegrationsButton";
import TagButton from "./TagButton";

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
    <header className="flex items-center gap-2 border-b border-black/10 dark:border-white/10 px-4 h-12 shrink-0">
      <AppLogo />

      {available && (
        <>
          <span className="mx-1 h-5 w-px bg-black/10 dark:bg-white/10" />
          <IntegrationTitle />
          <FolderPicker />
        </>
      )}

      <div className="ml-auto flex items-center gap-2">
        {available && (
          <>
            <IntegrationsButton />
            <TagButton getIntegrationId={getIntegrationId} />
            <SaveButton />
          </>
        )}
        <RunBar />
        {userMenu}
      </div>
    </header>
  );
}
