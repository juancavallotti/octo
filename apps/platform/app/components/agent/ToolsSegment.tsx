"use client";

import ToolChip from "./ToolChip";
import type { ToolRun } from "./turns";

/**
 * One round of tool calls — everything the agent asked for before it went back to
 * the model.
 *
 * The grouping is the point rather than the styling: a run that calls two tools,
 * reads the answers and then calls two more did two rounds, and the second round
 * is a decision it made because of the first. Flattened into one list, that
 * reasoning is invisible and the run reads as four things done at once.
 */
export default function ToolsSegment({
  runs,
  onAuthorize,
}: {
  runs: ToolRun[];
  onAuthorize: (id: string, allow: boolean) => void;
}) {
  return (
    <div className="flex flex-col gap-1">
      {runs.map((run) => (
        <ToolChip key={run.id} run={run} onAuthorize={onAuthorize} />
      ))}
    </div>
  );
}
