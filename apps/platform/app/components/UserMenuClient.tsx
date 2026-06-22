"use client";

import { useEffect, useRef, useState } from "react";
import { LogOut } from "lucide-react";

/** Initials fallback for when the IdP returns no picture. */
function initials(name: string | null, email: string | null): string {
  const base = (name || email || "").trim();
  if (!base) return "?";
  const parts = base.split(/[\s@._-]+/).filter(Boolean);
  const first = parts[0]?.[0] ?? "?";
  const second = parts.length > 1 ? (parts[1][0] ?? "") : "";
  return (first + second).toUpperCase();
}

/**
 * Account control: the user's avatar (standard profile picture, or initials when
 * none) that opens a dropdown with their name/email and a sign-out action. Closes
 * on outside click or Escape, mirroring the other launcher popovers.
 */
export default function UserMenuClient({
  name,
  email,
  image,
  signOutAction,
}: {
  name: string | null;
  email: string | null;
  image: string | null;
  signOutAction: () => Promise<void>;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const label = name || email || "Account";

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        title={label}
        onClick={() => setOpen((o) => !o)}
        className="flex h-7 w-7 items-center justify-center overflow-hidden rounded-full ring-1 ring-black/10 transition-[box-shadow] hover:ring-black/30 dark:ring-white/15 dark:hover:ring-white/30"
      >
        {image ? (
          // eslint-disable-next-line @next/next/no-img-element -- avatar host is the IdP/CDN, not known at build time
          <img
            src={image}
            alt=""
            referrerPolicy="no-referrer"
            className="h-full w-full object-cover"
          />
        ) : (
          <span className="flex h-full w-full items-center justify-center bg-zinc-200 text-[11px] font-medium text-zinc-600 dark:bg-zinc-700 dark:text-zinc-200">
            {initials(name, email)}
          </span>
        )}
      </button>

      {open && (
        <div
          role="menu"
          className="absolute right-0 top-full z-30 mt-2 w-60 overflow-hidden rounded-xl border border-black/10 bg-white shadow-lg dark:border-white/10 dark:bg-zinc-900"
        >
          <div className="flex flex-col gap-0.5 border-b border-black/5 px-3 py-2.5 dark:border-white/5">
            {name && (
              <span className="truncate text-sm font-medium" title={name}>
                {name}
              </span>
            )}
            {email && (
              <span
                className="truncate text-xs text-zinc-500 dark:text-zinc-400"
                title={email}
              >
                {email}
              </span>
            )}
          </div>
          <form action={signOutAction}>
            <button
              type="submit"
              role="menuitem"
              className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-zinc-700 hover:bg-black/5 dark:text-zinc-200 dark:hover:bg-white/10"
            >
              <LogOut className="h-4 w-4" />
              Sign out
            </button>
          </form>
        </div>
      )}
    </div>
  );
}
