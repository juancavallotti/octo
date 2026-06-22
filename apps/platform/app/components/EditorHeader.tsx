"use client";

import Image from "next/image";
import {
  useFileSystem,
  RunBar,
  IntegrationTitle,
  FolderPicker,
  SaveButton,
} from "@octo/editor";
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
      {/* h-6 w-auto controls both axes so Tailwind's `img { height: auto }`
          reset doesn't trigger Next's aspect-ratio warning. */}
      <Image
        src="/octo-logo.png"
        alt="Octo logo"
        width={24}
        height={24}
        className="h-6 w-auto"
        priority
      />
      <span className="font-semibold tracking-tight">Octo</span>

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
            <SaveButton
              onSaved={(stored) =>
                // Promote the address bar to the bookmarkable /i/<id> URL without
                // remounting the editor (Next syncs the router for manual updates).
                window.history.replaceState(null, "", `/i/${stored.id}`)
              }
            />
          </>
        )}
        <RunBar />
        {userMenu}
      </div>
    </header>
  );
}
