import type { LucideIcon } from "lucide-react";

export interface PaletteItemProps {
  label: string;
  icon: LucideIcon;
  selected?: boolean;
  onSelect?: () => void;
}

/**
 * Reusable presentational row: an icon next to a label, rendered as a button.
 * Pure/stateless — selection is controlled by the parent. Part of the shared
 * component library under components/ui.
 */
export default function PaletteItem({
  label,
  icon: Icon,
  selected = false,
  onSelect,
}: PaletteItemProps) {
  return (
    <button
      type="button"
      aria-pressed={selected}
      onClick={onSelect}
      className={[
        "w-full flex items-center gap-3 rounded-md px-3 py-2 text-sm text-left transition-colors",
        selected
          ? "bg-black/10 dark:bg-white/15"
          : "hover:bg-black/5 dark:hover:bg-white/10",
      ].join(" ")}
    >
      <Icon size={18} className="text-zinc-500 shrink-0" />
      <span>{label}</span>
    </button>
  );
}
