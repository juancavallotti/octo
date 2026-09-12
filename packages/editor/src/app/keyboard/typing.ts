/**
 * Is this event coming from somewhere the user is typing?
 *
 * Every editor-wide shortcut needs this and none of them can skip it: the canvas is
 * full of inline name fields and settings inputs, and a shortcut bound on `document`
 * sees their keystrokes too. "-" is a character in a block name before it is zoom-out,
 * and Backspace is a character before it is delete-the-selected-block.
 *
 * One function rather than the check repeated at each listener, because the listeners
 * have to agree and the failure when they disagree is silent — a shortcut that eats
 * a keystroke in one field and not another.
 */
export function isTypingTarget(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null;
  return !!(
    el?.isContentEditable ||
    el instanceof HTMLInputElement ||
    el instanceof HTMLTextAreaElement ||
    el instanceof HTMLSelectElement
  );
}
