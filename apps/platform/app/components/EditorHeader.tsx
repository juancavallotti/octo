"use client";

import {
  useFileSystem,
  LayoutToggles,
  RunBar,
  ViewModeToggle,
  SaveButton,
} from "@octo/editor";
import AppLogo from "./AppLogo";
import IntegrationPicker from "./IntegrationPicker";
import IntegrationsButton from "./IntegrationsButton";
import MoreMenu from "./MoreMenu";
import DeployButton from "./DeployButton";

/**
 * The editor's top bar. The integration controls (picker, Deploy, Save, manage and
 * the ⋮ overflow) only appear when a filesystem capability is present (`useFileSystem()`);
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
          {/* Which integration is open, in the corner the desktop shell keeps its
              folder in. Its name is edited in the document bar below, next to the
              file switcher, exactly as the standalone does it. */}
          <IntegrationPicker />

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
            {/* Duplicate and Tag: real, but rare. Behind the ⋮, and the ⋮ next to
                Manage — they are all "things you do to this integration", while
                Deploy, Save and RUN act on what is on screen. */}
            <MoreMenu getIntegrationId={getIntegrationId} />
            <DeployButton getIntegrationId={getIntegrationId} />
            <SaveButton />
          </>
        )}
        <RunBar />
        {/* Last on the bar, VS Code's corner: about the window, not the file. */}
        <span className="mx-1 h-5 w-px bg-black/10 dark:bg-white/10" />
        <LayoutToggles />
        {userMenu}
      </div>
    </header>
  );
}
