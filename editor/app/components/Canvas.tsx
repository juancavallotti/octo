"use client";

import FlowBoard from "./FlowBoard";

/**
 * Canvas is the main flow-editing area: a scrollable dot-grid surface that hosts
 * all the file's flows stacked vertically (FlowBoard).
 */
export default function Canvas() {
  return (
    <main className="relative flex-1 min-w-0 overflow-auto canvas-grid">
      <FlowBoard />
    </main>
  );
}
