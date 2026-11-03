"use client";

import { ROLE_LABELS } from "@/app/auth/roles";
import type { ReactNode } from "react";
import HoverCard from "@/app/components/HoverCard";

/**
 * The roles one person holds, as a row of pills.
 *
 * A reading of the row and not a control: changing what somebody may do happens
 * in the edit dialog, where it takes a deliberate open-tick-save rather than one
 * click on a list somebody was scrolling.
 *
 * Only what they hold, because that is what the column is for. What they could
 * hold is a question the dialog answers, with the catalogue's own description of
 * each role beside it.
 */
export default function RoleBadges({
  roles,
  describe,
}: {
  roles: string[];
  /** A hover card for a role; a role it returns nothing for gets none. */
  describe?: (role: string) => ReactNode;
}) {
  if (roles.length === 0) {
    return <span className="text-xs text-zinc-400 italic">No roles</span>;
  }
  return (
    <div className="flex flex-wrap gap-1">
      {roles.map((role) => {
        const pill = (cardId?: string) => (
          <span
            aria-describedby={cardId}
            className="rounded-full bg-zinc-900 px-2 py-0.5 text-xs text-white dark:bg-white dark:text-zinc-900"
          >
            {ROLE_LABELS[role] ?? role}
          </span>
        );
        const card = describe?.(role);
        return card ? (
          <HoverCard key={role} card={card}>
            {pill}
          </HoverCard>
        ) : (
          <span key={role}>{pill()}</span>
        );
      })}
    </div>
  );
}
