"use client";

import { RunBar, SaveButton } from "@octo/editor";
import StandaloneFileMenu from "./StandaloneFileMenu";

/**
 * The standalone editor's top bar: the product mark, an open/new file menu and
 * Save (local-disk filesystem), and the RUN control. No orchestrator, auth, or
 * folders. `current` is the open file's id, shown in the menu trigger.
 */
export default function StandaloneHeader({ current }: { current?: string }) {
  return (
    <header className="flex items-center gap-2 border-b border-black/10 dark:border-white/10 px-4 h-12 shrink-0">
      <span className="font-semibold tracking-tight">Octo</span>
      <span className="rounded bg-black/[0.06] px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-zinc-500 dark:bg-white/10">
        standalone
      </span>
      <span className="mx-1 h-5 w-px bg-black/10 dark:bg-white/10" />
      <StandaloneFileMenu current={current} />

      <div className="ml-auto flex items-center gap-2">
        <SaveButton
          onSaved={(stored) =>
            // Reflect the open file in the URL so a reload reopens it; a new file
            // gets its freshly created id.
            window.history.replaceState(
              null,
              "",
              `/?file=${encodeURIComponent(stored.id)}`,
            )
          }
        />
        <RunBar />
      </div>
    </header>
  );
}
