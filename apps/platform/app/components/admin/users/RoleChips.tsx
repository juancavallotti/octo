"use client";

import { ROLE_LABELS } from "@/app/auth/roles";
import type { RoleOption } from "@/app/model/users";

/**
 * The roles one person holds, as a row of toggles.
 *
 * Every role in the catalogue is shown rather than only the granted ones, so
 * changing what somebody may do is one click and not a menu — and so a role
 * nobody holds yet is still discoverable.
 */
export default function RoleChips({
  held,
  catalogue,
  disabled,
  disabledReason,
  onToggle,
}: {
  held: string[];
  catalogue: RoleOption[];
  disabled?: boolean;
  /** Said on hover when the toggles are disabled. */
  disabledReason?: string;
  onToggle: (role: string, grant: boolean) => void;
}) {
  return (
    <div className="flex flex-wrap gap-1">
      {catalogue.map(({ role, description }) => {
        const on = held.includes(role);
        return (
          <button
            key={role}
            type="button"
            disabled={disabled}
            aria-pressed={on}
            title={disabled ? disabledReason : description}
            onClick={() => onToggle(role, !on)}
            className={
              "rounded-full border px-2 py-0.5 text-xs transition-colors disabled:opacity-50 " +
              (on
                ? "border-transparent bg-zinc-900 text-white dark:bg-white dark:text-zinc-900"
                : "border-black/15 text-zinc-600 hover:bg-black/5 dark:border-white/20 dark:text-zinc-300 dark:hover:bg-white/10")
            }
          >
            {ROLE_LABELS[role] ?? role}
          </button>
        );
      })}
    </div>
  );
}
