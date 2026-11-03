"use client";

import { type ReactNode, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Check, ChevronDown } from "lucide-react";

/** The menu's width in pixels: w-56. */
const MENU_WIDTH = 224;

/**
 * A button that opens a menu of choices: the shared shape of the dialogs'
 * pickers. The menu is rendered on the document body, placed against the
 * button, so a scrolling container around the button — a dialog's body — cannot
 * clip it. It closes on a choice, an outside click, Escape, scroll or resize.
 */
export default function MenuButton({
  icon,
  label,
  title,
  ariaLabel,
  disabled,
  align = "end",
  children,
}: {
  icon: ReactNode;
  label: ReactNode;
  title?: string;
  ariaLabel?: string;
  disabled?: boolean;
  /** Which edge of the button the menu lines up with: "end" opens leftward, for
   * a button near the right of the page; "start" opens rightward. */
  align?: "start" | "end";
  /** The menu's items, given the function that closes it. */
  children: (close: () => void) => ReactNode;
}) {
  // Where the menu sits, from the button's box when it opened; null is closed.
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null);
  const open = pos !== null;
  const buttonRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const close = () => setPos(null);

  const toggle = () => {
    if (open) {
      close();
      return;
    }
    const box = buttonRef.current?.getBoundingClientRect();
    if (!box) return;
    setPos({
      top: box.bottom + 4,
      left: align === "start" ? box.left : box.right - MENU_WIDTH,
    });
  };

  useEffect(() => {
    if (!open) return;
    const dismiss = () => setPos(null);
    const onDown = (e: MouseEvent) => {
      const target = e.target as Node;
      if (
        !buttonRef.current?.contains(target) &&
        !menuRef.current?.contains(target)
      )
        dismiss();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") dismiss();
    };
    // A menu placed against a button that moved would float in the wrong place.
    const onScroll = (e: Event) => {
      if (!menuRef.current?.contains(e.target as Node)) dismiss();
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    window.addEventListener("scroll", onScroll, true);
    window.addEventListener("resize", dismiss);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
      window.removeEventListener("scroll", onScroll, true);
      window.removeEventListener("resize", dismiss);
    };
  }, [open]);

  return (
    <div className="shrink-0">
      <button
        ref={buttonRef}
        type="button"
        onClick={toggle}
        disabled={disabled}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={ariaLabel}
        title={title}
        className="inline-flex items-center gap-1.5 rounded-md border border-black/10 bg-transparent px-2.5 py-1 text-sm font-medium transition-colors hover:bg-black/[0.04] disabled:opacity-50 dark:border-white/15 dark:hover:bg-white/[0.06]"
      >
        {icon}
        <span className="max-w-[10rem] truncate">{label}</span>
        <ChevronDown size={14} className="text-zinc-400" />
      </button>

      {pos &&
        createPortal(
          <div
            ref={menuRef}
            role="menu"
            style={{ top: pos.top, left: pos.left, width: MENU_WIDTH }}
            className="fixed z-[60] max-h-72 overflow-y-auto rounded-lg border border-black/10 bg-white py-1 shadow-lg dark:border-white/10 dark:bg-zinc-900"
          >
            {children(close)}
          </div>,
          document.body,
        )}
    </div>
  );
}

/** One choice in a MenuButton's menu, checked when it is the current one. */
export function MenuItem({
  checked,
  onSelect,
  icon,
  trailing,
  children,
}: {
  checked: boolean;
  onSelect: () => void;
  icon?: ReactNode;
  trailing?: ReactNode;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      role="menuitemradio"
      aria-checked={checked}
      onClick={onSelect}
      className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-sm hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
    >
      <Check size={14} className={checked ? "text-sky-500" : "invisible"} />
      {icon}
      <span className="flex-1 truncate">{children}</span>
      {trailing}
    </button>
  );
}

/** A rule between groups of a MenuButton's items. */
export function MenuDivider() {
  return <div className="my-1 border-t border-black/5 dark:border-white/5" />;
}
