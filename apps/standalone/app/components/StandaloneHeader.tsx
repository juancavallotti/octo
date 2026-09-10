"use client";

import Image from "next/image";
import {
  LayoutToggles,
  RunBar,
  SaveButton,
  ViewModeToggle,
} from "@octo/editor";
import ModeBadge from "./ModeBadge";
import VaultChip from "./VaultChip";

/* onSaved (URL sync) lives on EditorRoot — see StandaloneEditor — so all save
   triggers (button, ⌘S, Enter in the rename field) share it. */

/**
 * The standalone editor's top bar: the Octo mark and the desktop shell's folder
 * chip, the view tabs centred on it, and — on the right — Save (local-disk
 * filesystem) and the RUN control. No orchestrator, auth, or folders.
 *
 * The open file is named and renamed in the document bar below, next to the file
 * switcher, rather than by a title field up here.
 */
export default function StandaloneHeader() {
  return (
    <header className="relative flex items-center gap-2 border-b border-black/10 dark:border-white/10 px-4 h-12 shrink-0">
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
      {/* Which folder is being served, next to the mark: it names the whole
          window, the way a project does. */}
      <VaultChip />

      {/* Centred on the bar itself, not on what is left over between the mark and
          the right-hand controls — the right side changes width with which
          controls the host offers, and a tab strip that drifts is worse than one
          that is simply in the middle. */}
      <div className="pointer-events-none absolute left-1/2 -translate-x-1/2">
        <div className="pointer-events-auto">
          <ViewModeToggle />
        </div>
      </div>

      <div className="ml-auto flex items-center gap-2">
        <SaveButton />
        <RunBar />
        {/* Last on the bar, VS Code's corner: these are about the window, not about
            the file or the run. */}
        <span className="mx-1 h-5 w-px bg-black/10 dark:bg-white/10" />
        <LayoutToggles />
      </div>
    </header>
  );
}
