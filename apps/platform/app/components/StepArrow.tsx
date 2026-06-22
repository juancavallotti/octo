import { ChevronDown } from "lucide-react";

/**
 * The vertical connector drawn between two stacked nodes: a short line capped
 * with a downward chevron, echoing the flow-direction arrows in the schematic.
 */
export default function StepArrow() {
  return (
    <div
      aria-hidden
      className="flex flex-col items-center text-zinc-400 dark:text-zinc-500"
    >
      <div className="h-4 w-px bg-zinc-300 dark:bg-zinc-600" />
      <ChevronDown size={16} className="-mt-1" />
    </div>
  );
}
