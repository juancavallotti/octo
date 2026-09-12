"use client";

import { ROLE_LABELS } from "@/app/auth/roles";
import type { RoleOption } from "@/app/model/users";

/**
 * Choosing what somebody may do, inside a dialog.
 *
 * Checkboxes with the catalogue's own sentence beside each one, rather than the
 * row of chips this replaced. Chips in the table made granting a role a single
 * click on a list somebody was reading, which is how an administrator hands out
 * `platform:admin` while meaning to scroll. Here the choice is deliberate: a
 * dialog was opened, a box was ticked, and a button was pressed.
 *
 * Every role in the catalogue is offered rather than only the granted ones, so a
 * role nobody holds yet is still discoverable.
 */
export default function RolePicker({
  catalogue,
  held,
  disabled,
  reason,
  onChange,
}: {
  catalogue: RoleOption[];
  held: string[];
  disabled?: boolean;
  /** Said in the dialog when the choice is refused — see UserModal. */
  reason?: string;
  onChange: (next: string[]) => void;
}) {
  const toggle = (role: string, on: boolean) =>
    onChange(on ? [...held, role] : held.filter((r) => r !== role));

  return (
    <fieldset className="flex flex-col gap-2" disabled={disabled}>
      <legend className="text-xs font-medium text-zinc-600 dark:text-zinc-300">
        Roles
      </legend>
      {catalogue.map(({ role, description }) => (
        <label key={role} className="flex items-start gap-2 text-sm">
          <input
            type="checkbox"
            checked={held.includes(role)}
            onChange={(e) => toggle(role, e.target.checked)}
            className="mt-0.5 accent-sky-500"
          />
          <span className="min-w-0">
            <span className="text-zinc-700 dark:text-zinc-200">
              {ROLE_LABELS[role] ?? role}
            </span>
            <span className="block text-xs text-zinc-500">{description}</span>
          </span>
        </label>
      ))}
      {/* Said here rather than in a title: a disabled control cannot take
          keyboard focus, and a tooltip is not reachable by touch at all. */}
      {reason && <p className="text-xs text-zinc-500">{reason}</p>}
    </fieldset>
  );
}
