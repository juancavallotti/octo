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
    <div className="flex h-9 shrink-0 items-center gap-0.5 border-b border-black/10 px-2 dark:border-white/10">
      <FileSwitcher />
      <div className="mx-1">
        <FlowsViewToggle />
      </div>
      <HistoryButtons />
      <ConnectionsLauncher />
      <EnvLauncher />
      <ResourcesLauncher />
      {files && <div className="ml-auto flex items-center">{files}</div>}
    </div>
  );
}
