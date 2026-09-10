/**
 * The shared look of a DocumentBar trigger.
 *
 * Its own module rather than a const on DocumentBar so the launchers can dress
 * themselves without importing the bar that renders them.
 *
 * Flat and quiet on purpose: these used to be floating pills over the canvas,
 * which is what a control needs when it hovers over content it does not belong
 * to. Inside a real bar the border, shadow and blur are noise, and the padding
 * they carried cost a row of canvas for three buttons.
 */
export const BAR_BUTTON =
  "flex items-center gap-1.5 rounded-md px-2 py-1 text-[13px] text-zinc-600 transition-colors hover:bg-black/[0.05] hover:text-zinc-900 dark:text-zinc-300 dark:hover:bg-white/[0.08] dark:hover:text-zinc-100";

/** The count pill a launcher shows when the document has some of its thing. */
export const BAR_COUNT =
  "rounded-full bg-black/[0.06] px-1.5 text-[11px] tabular-nums dark:bg-white/10";
