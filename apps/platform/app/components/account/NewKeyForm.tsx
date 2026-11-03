"use client";

import { useState } from "react";
import { Plus } from "lucide-react";
import { useRoles } from "@/app/auth/RolesContext";
import { KEY_ROLES, ROLE_LABELS, mayDelegate } from "@/app/auth/roles";
import HoverCard from "@/app/components/HoverCard";
import { keyRoleCard } from "./keyRoles";

/**
 * The form that mints an API key: a label, the date it expires, and the roles it
 * carries. Only roles the signed-in user holds are offered (any, for an admin),
 * and never admin;
 * iam enforces both, so this only keeps a person from asking for what it would
 * refuse. Clears itself once onCreate reports success.
 */

const INPUT =
  "rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

const PRESET_DAYS = [7, 30, 90, 365];
/** iam refuses an expiry more than two years out. */
const MAX_DAYS = 730;

/** The local calendar date `days` from today, as a date input's YYYY-MM-DD value. */
function dateIn(days: number): string {
  const d = new Date();
  d.setDate(d.getDate() + days);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/**
 * The instant a key picked for a YYYY-MM-DD local date expires: that date at the
 * current time of day, so "7 days" is seven days from now.
 */
function expiryOf(date: string): Date {
  const [y, m, d] = date.split("-").map(Number);
  const at = new Date();
  at.setFullYear(y, m - 1, d);
  return at;
}

/** Whether a date picked now falls inside the window iam accepts. */
function inWindow(date: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) return false;
  return date >= dateIn(1) && date <= dateIn(MAX_DAYS);
}

export default function NewKeyForm({
  busy,
  onCreate,
}: {
  busy: boolean;
  /** Mint the key; resolves to whether it worked. */
  onCreate: (name: string, roles: string[], expiresAt: string) => Promise<boolean>;
}) {
  const { roles: held } = useRoles();
  const offered = KEY_ROLES.filter((r) => mayDelegate(held, r));
  const [name, setName] = useState("");
  const [roles, setRoles] = useState<string[]>([]);
  const [expires, setExpires] = useState(() => dateIn(30));

  const canAdd = !busy && name.trim().length > 0 && roles.length > 0 && inWindow(expires);

  const toggle = (role: string) =>
    setRoles((rs) => (rs.includes(role) ? rs.filter((r) => r !== role) : [...rs, role]));

  const submit = async () => {
    // Checked again here: a form left open past midnight keeps a stale canAdd.
    if (!canAdd || !inWindow(expires)) return;
    if (await onCreate(name.trim(), roles, expiryOf(expires).toISOString())) {
      setName("");
      setRoles([]);
    }
  };

  return (
    <div className="mt-4 flex flex-wrap items-center gap-2 rounded-lg border border-black/10 p-3 dark:border-white/10">
      <input
        value={name}
        disabled={busy}
        placeholder="Key name (e.g. laptop, ci)"
        onChange={(e) => setName(e.target.value)}
        onKeyDown={(e) => e.key === "Enter" && submit()}
        className={`${INPUT} min-w-[12rem] flex-1`}
      />
      <input
        type="date"
        value={expires}
        min={dateIn(1)}
        max={dateIn(MAX_DAYS)}
        disabled={busy}
        onChange={(e) => setExpires(e.target.value)}
        aria-label="Expires on"
        className={INPUT}
      />
      <button
        type="button"
        onClick={submit}
        disabled={!canAdd}
        className="inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white transition-colors hover:bg-sky-500 disabled:opacity-50"
      >
        <Plus size={14} />
        Create
      </button>
      <div className="flex w-full flex-wrap items-center gap-1 text-xs text-zinc-500">
        <span>Expires in</span>
        {PRESET_DAYS.map((d) => (
          <button
            key={d}
            type="button"
            disabled={busy}
            onClick={() => setExpires(dateIn(d))}
            className="rounded px-1.5 py-0.5 hover:bg-black/5 dark:hover:bg-white/10"
          >
            {d} days
          </button>
        ))}
      </div>
      <fieldset className="flex w-full flex-wrap items-center gap-3 text-sm">
        <legend className="sr-only">Roles</legend>
        <span className="text-xs text-zinc-500">Roles</span>
        {offered.length === 0 ? (
          <span className="text-xs text-zinc-400 italic">
            You hold no role a key can carry.
          </span>
        ) : (
          offered.map((r) => (
            <HoverCard key={r} card={keyRoleCard(r)}>
              {(cardId) => (
                <label className="inline-flex items-center gap-1.5">
                  <input
                    type="checkbox"
                    aria-describedby={cardId}
                    checked={roles.includes(r)}
                    disabled={busy}
                    onChange={() => toggle(r)}
                  />
                  {ROLE_LABELS[r] ?? r}
                </label>
              )}
            </HoverCard>
          ))
        )}
      </fieldset>
    </div>
  );
}
