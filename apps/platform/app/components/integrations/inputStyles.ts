/**
 * The bare input styling the deploy and rollout dialogs share.
 *
 * It is not a component: `Field` already wraps an input with its label, and these
 * are the classes for the input inside it. Both dialogs carried the same string
 * verbatim, which is one edit away from two dialogs that no longer match.
 */
export const INPUT =
  "rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";
