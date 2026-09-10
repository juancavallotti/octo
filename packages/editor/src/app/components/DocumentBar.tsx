"use client";

import ConnectionsLauncher from "./ConnectionsLauncher";
import EnvLauncher from "./EnvLauncher";
import ResourcesLauncher from "./ResourcesLauncher";

/**
 * The document bar: everything that belongs to the open file rather than to the
 * flow you happen to be looking at — its connections, environment and resources
 * on the left, and the host's file switcher on the right.
 *
 * These three used to float over the top-left of the canvas as separate pills,
 * which read as three unrelated controls dropped on the drawing and only existed
 * in the canvas view. As a bar they are one group, they cost a strip of chrome
 * instead of a corner of the canvas, and they stay put when you switch to YAML or
 * Testing — where the file's connections are just as much the subject.
 *
 * `files` is a host slot, and its absence is the feature: the platform has its own
 * integration browser and passes nothing, so the right of the bar is simply empty
 * there.
 */
export default function DocumentBar({ files }: { files?: React.ReactNode }) {
  return (
    <div className="flex h-9 shrink-0 items-center gap-0.5 border-b border-black/10 px-2 dark:border-white/10">
      <ConnectionsLauncher />
      <EnvLauncher />
      <ResourcesLauncher />
      {files && <div className="ml-auto flex items-center">{files}</div>}
    </div>
  );
}
