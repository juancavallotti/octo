import type { LucideIcon } from "lucide-react";
import { listBlocks, resolveIcon } from "@/app/schema";

export interface PaletteComponent {
  /** Block type, e.g. "log" — matches a schema BlockSpec and a model BlockNode. */
  id: string;
  label: string;
  icon: LucideIcon;
}

/**
 * The palette of integration building blocks, derived from the runtime
 * capability schema (app/schema). Add blocks by extending capabilities.json, not
 * this file.
 */
export const PALETTE: PaletteComponent[] = listBlocks().map((block) => ({
  id: block.type,
  label: block.label,
  icon: resolveIcon(block.icon),
}));
