/**
 * The Platform window's controls, in the editor's own vocabulary: Save's filled sky
 * for the one thing a section is for, a quiet bordered button for the rest.
 */
export const PRIMARY =
  "inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white hover:bg-sky-500 disabled:cursor-not-allowed disabled:opacity-40";

export const SECONDARY =
  "inline-flex items-center gap-1.5 rounded-md border border-black/10 px-2.5 py-1 text-sm text-zinc-700 transition-colors hover:bg-black/[0.04] disabled:cursor-not-allowed disabled:opacity-40 dark:border-white/15 dark:text-zinc-200 dark:hover:bg-white/[0.06]";

export const DANGER =
  "inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-sm text-rose-600 transition-colors hover:bg-rose-500/10 disabled:opacity-40 dark:text-rose-400";

export const INPUT =
  "min-w-0 rounded-md border border-black/10 bg-transparent px-2 py-1 text-sm outline-none placeholder:text-zinc-400 focus:border-sky-500 dark:border-white/15";

export const CARD = "rounded-xl border border-black/10 dark:border-white/10";

export const SECTION_TITLE = "text-xs font-semibold uppercase tracking-wide text-zinc-500";

export const MUTED = "text-xs text-zinc-500 dark:text-zinc-400";

/** A date the platform sent, as a reader wants it. */
export function when(iso: string | undefined): string {
  if (!iso) return "";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "" : d.toLocaleString();
}
