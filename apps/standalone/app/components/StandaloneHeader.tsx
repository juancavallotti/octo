"use client";

import Image from "next/image";
import {
  IntegrationTitle,
  RunBar,
  SaveButton,
  ViewModeToggle,
} from "@octo/editor";
import StandaloneFileMenu from "./StandaloneFileMenu";
import ModeBadge from "./ModeBadge";
import VaultChip from "./VaultChip";

/* onSaved (URL sync) lives on EditorRoot — see StandaloneEditor — so all save
   triggers (button, ⌘S, Enter in the title) share it. */

/**
 * The standalone editor's top bar: the Octo mark, an editable flow title (its
 * name becomes the `*.yaml` filename on the first save), an open/new file menu,
 * Save (local-disk filesystem), and the RUN control. No orchestrator, auth, or
 * folders.
 */
export default function StandaloneHeader() {
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
      <ModeBadge />
      <span className="mx-1 h-5 w-px bg-black/10 dark:bg-white/10" />
      {/* Which folder is being served. Every deployment has one and none of them
          used to say so. */}
      <VaultChip />
      <span className="mx-1 h-5 w-px bg-black/10 dark:bg-white/10" />
      <IntegrationTitle />
      <StandaloneFileMenu />
      <ViewModeToggle />

      <div className="ml-auto flex items-center gap-2">
        <SaveButton />
        <RunBar />
      </div>
    </header>
  );
}
