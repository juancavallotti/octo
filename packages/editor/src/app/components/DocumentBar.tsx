"use client";

import ConnectionsLauncher from "./ConnectionsLauncher";
import HistoryButtons from "./HistoryButtons";
import EnvLauncher from "./EnvLauncher";
import ResourcesLauncher from "./ResourcesLauncher";

/**
 * The document bar: everything that belongs to the open file rather than to the
 * flow you happen to be looking at — undo/redo and its connections, environment and
 * resources on the left, and the host's file switcher on the right.
 *
 * `files` is a slot, and its absence is the feature: a caller with its own file browser
 * passes nothing and the right of the bar is empty.
 */
export default function DocumentBar({ files }: { files?: React.ReactNode }) {
  return (
    <div className="flex h-9 shrink-0 items-center gap-0.5 border-b border-black/10 px-2 dark:border-white/10">
      <HistoryButtons />
      <ConnectionsLauncher />
      <EnvLauncher />
      <ResourcesLauncher />
      {files && <div className="ml-auto flex items-center">{files}</div>}
    </div>
  );
}
