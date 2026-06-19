"use client";

import type { LucideIcon } from "lucide-react";
import { useDraggable } from "@dnd-kit/core";
import { PaletteItem } from "@/components/ui";

export interface PaletteBlockProps {
  type: string;
  label: string;
  icon: LucideIcon;
  onAdd: () => void;
}

/**
 * A palette entry: draggable onto the canvas to add a block, and clickable to
 * append one. The drag listeners live on a wrapper so the inner PaletteItem
 * button keeps handling clicks (the pointer sensor only starts a drag after a
 * small movement, so a plain click still adds).
 */
export default function PaletteBlock({
  type,
  label,
  icon,
  onAdd,
}: PaletteBlockProps) {
  const { setNodeRef, listeners, isDragging } = useDraggable({
    id: `palette-${type}`,
    data: { source: "palette", blockType: type },
  });

  return (
    <div
      ref={setNodeRef}
      {...listeners}
      className={isDragging ? "opacity-50" : undefined}
    >
      <PaletteItem label={label} icon={icon} onSelect={onAdd} />
    </div>
  );
}
