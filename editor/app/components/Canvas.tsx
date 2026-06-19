"use client";

import FlowBoard from "./FlowBoard";
import ConnectionsLauncher from "./ConnectionsLauncher";

/**
 * Canvas is the main flow-editing area: a scrollable dot-grid surface that hosts
 * all the file's flows stacked vertically (FlowBoard). The connections launcher is
 * pinned to the top-left as an overlay outside the scroll area, so it stays put as
 * the flows scroll.
 */
export default function Canvas() {
  return (
    <div className="relative flex-1 min-w-0">
      <main className="absolute inset-0 overflow-auto canvas-grid">
        <FlowBoard />
      </main>
      <div className="absolute left-4 top-4 z-30">
        <ConnectionsLauncher />
      </div>
    </div>
  );
}
