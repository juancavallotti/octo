"use client";

import Image from "next/image";
import {
  LayoutToggles,
  RunBar,
  SaveButton,
  ViewModeToggle,
} from "@octo/editor";
import ModeBadge from "./ModeBadge";
import PlatformChip from "./PlatformChip";
import PushButton from "./PushButton";
import UpdateChip from "./UpdateChip";
import VaultChip from "./VaultChip";
import { useDesktopMenu } from "./useDesktopMenu";

/* onSaved (URL sync) lives on EditorRoot — see StandaloneEditor — so all save
   triggers (button, ⌘S, Enter in the rename field) share it. */

/**
 * The editor's top bar: the Octo mark, the folder chip and its platform chip, the
 * view tabs centred on it, and Save, Push and the RUN control on the right. The open file is named and renamed in
 * the document bar below, not here.
 */
export default function StandaloneHeader() {
  // Here because the header is the shell's piece of the window, and inside the editor.
  useDesktopMenu();
  return (
    // Three regions: the two sides share what the tabs leave equally, so the tabs sit
    // in the middle while there is room. The right side never shrinks below its
    // controls; when it needs more than its share, the left side gives way — the
    // folder name truncates — and the tabs move over with it rather than under them.
    <header className="flex items-center gap-2 border-b border-black/10 dark:border-white/10 px-4 h-12 shrink-0">
      <div className="flex min-w-0 flex-1 basis-0 items-center gap-2">
        {/* h-6 w-auto controls both axes so Tailwind's `img { height: auto }`
            reset doesn't trigger Next's aspect-ratio warning. */}
        <Image
          src="/octo-logo.png"
          alt="Octo logo"
          width={24}
          height={24}
          className="h-6 w-auto shrink-0"
          priority
        />
        <span className="shrink-0 font-semibold tracking-tight">Octo</span>
        <ModeBadge />
        <span className="mx-1 h-5 w-px shrink-0 bg-black/10 dark:bg-white/10" />
        {/* Which folder is being served, next to the mark: it names the whole
            window, the way a project does. */}
        <VaultChip />
        {/* Which integration that folder is on the platform, when it is one. */}
        <PlatformChip />
      </div>

      <ViewModeToggle />

      <div className="flex min-w-max flex-1 basis-0 items-center justify-end gap-2">
        {/* Before the file's own controls: it is about the app, not the document,
            and it is only ever there when there is something to say. */}
        <UpdateChip />
        <SaveButton />
        <PushButton />
        <RunBar />
        {/* Last on the bar, VS Code's corner: these are about the window, not about
            the file or the run. */}
        <span className="mx-1 h-5 w-px bg-black/10 dark:bg-white/10" />
        <LayoutToggles />
      </div>
    </header>
  );
}
