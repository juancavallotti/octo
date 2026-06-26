"use client";

import {
  SortableContext,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Workflow } from "lucide-react";
import type { Integration } from "@/app/model/orchestrator";
import type { DragData } from "./model";

/** The middle column: the selected bucket's integrations, selectable into the detail panel. */
interface Props {
  integrations: Integration[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  /** True when the current bucket is a folder, so cards can be reordered. */
  reorderable: boolean;
}

export default function IntegrationList({
  integrations,
  selectedId,
  onSelect,
  reorderable,
}: Props) {
  return (
    <div className="flex w-72 shrink-0 flex-col border-r border-black/10 dark:border-white/10">
      {integrations.length === 0 ? (
        <p className="px-4 py-4 text-sm text-zinc-400">No integrations here.</p>
      ) : (
        <ul className="min-h-0 flex-1 overflow-y-auto py-1">
          <SortableContext
            items={integrations.map((i) => `integration:${i.id}`)}
            strategy={verticalListSortingStrategy}
          >
            {integrations.map((i) => (
              <IntegrationCard
                key={i.id}
                integration={i}
                selected={selectedId === i.id}
                onSelect={onSelect}
                reorderable={reorderable}
              />
            ))}
          </SortableContext>
        </ul>
      )}
    </div>
  );
}

/**
 * One integration row. It is always a drag source (drag it onto a folder/Unfiled
 * in the tree to file/unfile it) and, inside a folder bucket, sorts among its peers
 * to reorder them. A plain click still selects it (a small pointer activation
 * distance keeps clicks and drags distinct).
 */
function IntegrationCard({
  integration: i,
  selected,
  onSelect,
  reorderable,
}: {
  integration: Integration;
  selected: boolean;
  onSelect: (id: string) => void;
  reorderable: boolean;
}) {
  const data: DragData = { kind: "integration", id: i.id, name: i.name };
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: `integration:${i.id}`, data });

  // While reordering inside a folder the card animates into place; when moving to
  // a folder the transform would drag the original away, so suppress it then.
  const style = reorderable
    ? { transform: CSS.Transform.toString(transform), transition }
    : undefined;

  return (
    <li ref={setNodeRef} style={style}>
      <button
        type="button"
        onClick={() => onSelect(i.id)}
        {...attributes}
        {...listeners}
        className={`flex w-full items-center gap-3 px-4 py-2 text-left ${
          isDragging ? "opacity-40" : ""
        } ${
          selected
            ? "bg-sky-500/10"
            : "hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
        }`}
      >
        <span
          className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-md ${
            selected
              ? "bg-sky-500/15 text-sky-600 dark:text-sky-400"
              : "bg-black/[0.04] text-zinc-500 dark:bg-white/[0.06] dark:text-zinc-400"
          }`}
        >
          <Workflow size={16} />
        </span>
        <span className="flex min-w-0 flex-col gap-0.5">
          <span className="truncate text-sm font-medium">{i.name}</span>
          <span className="text-xs text-zinc-400">
            {new Date(i.lastUpdated).toLocaleDateString()}
          </span>
        </span>
      </button>
    </li>
  );
}
