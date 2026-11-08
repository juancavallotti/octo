"use client";

import { useEffect, useState } from "react";
import { ROLE_LABELS } from "@/app/auth/roles";
import Callout from "@/app/components/ui/Callout";
import { heldIn } from "@/app/lib/environment";
import { listEnvironments, type Environment } from "@/app/model/environments";
import type { RoleOption } from "@/app/model/users";

const ADMIN = "platform:admin";

/**
 * What somebody may do in each environment other than the default, inside the
 * edit dialog beside the default's roles.
 *
 * Admin is not offered here: it is granted in the default and reaches every
 * environment, so an administrator is told so instead of being shown boxes
 * that would mean nothing. Nothing is shown while there are no other
 * environments.
 */
export default function EnvironmentRoles({
  catalogue,
  held,
  isAdmin,
  disabled,
  onChange,
}: {
  catalogue: RoleOption[];
  held: Record<string, string[]>;
  isAdmin: boolean;
  disabled?: boolean;
  onChange: (next: Record<string, string[]>) => void;
}) {
  const [environments, setEnvironments] = useState<Environment[]>([]);
  const [failed, setFailed] = useState<string | null>(null);
  useEffect(() => {
    let live = true;
    listEnvironments().then(
      (all) => live && setEnvironments(all.filter((e) => !e.default)),
      (err) => live && setFailed((err as Error).message),
    );
    return () => {
      live = false;
    };
  }, []);

  // Said rather than left blank: "no other environments" and "they could not be
  // read" look the same otherwise, and the second hides grants this person holds.
  if (failed) {
    return <Callout>Couldn&rsquo;t load the environments ({failed}). Close and reopen to try again.</Callout>;
  }
  if (environments.length === 0) return null;
  if (isAdmin) {
    return (
      <p className="text-xs text-zinc-500">
        Administrators hold every role in every environment.
      </p>
    );
  }

  const roles = catalogue.filter((r) => r.role !== ADMIN);
  const toggle = (environment: string, role: string, on: boolean) => {
    const current = heldIn(held, environment);
    onChange({
      ...held,
      [environment]: on ? [...current, role] : current.filter((r) => r !== role),
    });
  };

  return (
    <fieldset className="flex flex-col gap-3" disabled={disabled}>
      <legend className="text-xs font-medium text-zinc-600 dark:text-zinc-300">
        Roles in other environments
      </legend>
      {environments.map((e) => (
        <div key={e.name} role="group" aria-label={e.displayName || e.name}>
          <p className="text-xs font-medium text-zinc-700 dark:text-zinc-200">
            {e.displayName || e.name}
          </p>
          <div className="mt-1 flex flex-wrap gap-x-4 gap-y-1">
            {roles.map(({ role }) => (
              <label key={role} className="flex items-center gap-1.5 text-sm">
                <input
                  type="checkbox"
                  checked={heldIn(held, e.name).includes(role)}
                  onChange={(ev) => toggle(e.name, role, ev.target.checked)}
                  className="accent-sky-500"
                />
                {ROLE_LABELS[role] ?? role}
              </label>
            ))}
          </div>
        </div>
      ))}
    </fieldset>
  );
}

/** The grants and revocations that turn `before` into `after`, per environment. */
export function environmentRoleChanges(
  before: Record<string, string[]>,
  after: Record<string, string[]>,
): { environment: string; role: string; grant: boolean }[] {
  const out: { environment: string; role: string; grant: boolean }[] = [];
  for (const environment of new Set([...Object.keys(before), ...Object.keys(after)])) {
    const was = heldIn(before, environment);
    const now = heldIn(after, environment);
    for (const role of now.filter((r) => !was.includes(r))) out.push({ environment, role, grant: true });
    for (const role of was.filter((r) => !now.includes(r))) out.push({ environment, role, grant: false });
  }
  return out;
}
