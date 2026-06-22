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

/**
 * The editor's top bar. The integration controls (title, folder, Save, manage)
 * only appear when a filesystem capability is present (`useFileSystem()`);
 * otherwise the bar is just the logo and the RUN control.
 */
export default function EditorHeader({
  userMenu,
}: {
  /** Account control slot (server-rendered UserMenu); only visible when SSO is on. */
  userMenu?: React.ReactNode;
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
            <SaveButton />
          </>
        )}
        <RunBar />
        {userMenu}
      </div>
    </header>
  );
}
