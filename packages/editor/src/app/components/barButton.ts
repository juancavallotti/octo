/**
 * The shared look of a DocumentBar trigger. Its own module rather than a const on
 * DocumentBar so the launchers can dress themselves without importing the bar that
 * renders them.
 */
export const BAR_BUTTON =
  "flex items-center gap-1.5 rounded-md px-2 py-1 text-[13px] text-zinc-600 transition-colors hover:bg-black/[0.05] hover:text-zinc-900 dark:text-zinc-300 dark:hover:bg-white/[0.08] dark:hover:text-zinc-100";

/** The count pill a launcher shows when the document has some of its thing. */
export const BAR_COUNT =
  "rounded-full bg-black/[0.06] px-1.5 text-[11px] tabular-nums dark:bg-white/10";

/**
 * The text of a header control that has an icon beside it. Below 1100px the header
 * has room for the icons alone, so the text becomes screen-reader-only — still the
 * control's accessible name, and still in its tooltip — rather than pushing the
 * controls into each other.
 */
export const HEADER_LABEL = "max-[1100px]:sr-only";

/**
 * The Canvas/YAML labels, which name the mode the file is shown in and so keep their
 * words while there is room. The bar is as wide as the column between the palette and
 * the settings panel — which depends on which of those are open, not on the window —
 * so it answers to that column's width (EditorBody makes it a container): below 40rem
 * the labels are screen-reader-only, the icons and tooltips stay.
 */
export const BAR_VIEW_LABEL = "@max-[40rem]:sr-only";
