"use client";

import ConnectionsLauncher from "./ConnectionsLauncher";
import FileSwitcher from "./FileSwitcher";
import FlowsViewToggle from "./FlowsViewToggle";
import HistoryButtons from "./HistoryButtons";
import EnvLauncher from "./EnvLauncher";
import ResourcesLauncher from "./ResourcesLauncher";

/**
 * The document bar: everything that belongs to the project and its open file rather
 * than to the flow you happen to be looking at — the file switcher and, beside it, how
 * that file is shown (canvas or YAML), undo/redo, the connections, environment and
 * resources on the left, and whatever the host adds on the right.
 *
 * `files` is a slot, and its absence is the feature: a host with nothing to add passes
 * nothing and the right of the bar is empty.
 */
export default function DocumentBar({ files }: { files?: React.ReactNode }) {
  return (
    // Wraps rather than overflowing: with the panels open on a narrow window the
    // column can be narrower than even the icon-only launchers, and a second row
    // beats a bar that runs under the settings panel or clips its own popovers.
    <div className="flex min-h-9 min-w-0 shrink-0 flex-wrap items-center gap-0.5 border-b border-black/10 px-2 dark:border-white/10">
      <FileSwitcher />
      <div className="mx-1">
        <FlowsViewToggle />
      </div>
      <HistoryButtons />
      <ConnectionsLauncher />
      <EnvLauncher />
      <ResourcesLauncher />
      {/* A long name truncates; past a readable minimum the slot wraps instead. */}
      {files && (
        <div className="ml-auto flex min-w-[8rem] max-w-full items-center">{files}</div>
      )}
    </div>
  );
}
