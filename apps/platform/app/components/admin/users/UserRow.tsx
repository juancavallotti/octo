"use client";

import { useState } from "react";
import { Trash2 } from "lucide-react";
import { PLATFORM_ADMIN } from "@/app/auth/roles";
import {
  deleteUser,
  grantRole,
  revokeRole,
  type PlatformUser,
  type RoleOption,
} from "@/app/model/users";
import RoleChips from "./RoleChips";

/**
 * One person: who they are, what they may do, and a way to remove them.
 *
 * Their own row is treated differently in one place. An administrator editing
 * their own roles is how somebody locks themselves out of the section they are
 * standing in, so the toggles are disabled there and say why. iam refuses to
 * remove the last administrator regardless — this only stops the attempt being
 * made by accident, which is the difference between a rule and a trap.
 */
export default function UserRow({
  user,
  roles,
  isSelf,
  onChanged,
  onRemoved,
  onError,
}: {
  user: PlatformUser;
  roles: RoleOption[];
  isSelf: boolean;
  onChanged: (user: PlatformUser) => void;
  onRemoved: () => void;
  onError: (message: string) => void;
}) {
  // One change at a time. Each call answers with the whole user, so two in flight
  // together can land out of order and the older reply would overwrite the newer
  // state — the row would end up showing something nobody asked for.
  const [busy, setBusy] = useState(false);

  const toggle = async (role: string, grant: boolean) => {
    setBusy(true);
    try {
      onChanged(await (grant ? grantRole(user.id, role) : revokeRole(user.id, role)));
    } catch (e) {
      onError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    setBusy(true);
    try {
      await deleteUser(user.id);
      onRemoved();
    } catch (e) {
      onError((e as Error).message);
      setBusy(false);
    }
  };

  return (
    <tr className="border-t border-black/5 dark:border-white/10">
      <td className="py-2 pr-4 align-top">
        <div className="text-sm">{user.name || "—"}</div>
        <div className="text-xs text-zinc-500">{user.email}</div>
      </td>
      <td className="py-2 pr-4 align-top">
        <RoleChips
          held={user.roles}
          catalogue={roles}
          disabled={isSelf || busy}
          onToggle={toggle}
        />
        {isSelf && (
          // Said in the row rather than in a title: a disabled control cannot take
          // keyboard focus, and a tooltip is not reachable by touch at all.
          <p className="mt-1 text-xs text-zinc-500">
            You cannot change your own roles or remove yourself.
          </p>
        )}
      </td>
      <td className="py-2 pr-4 align-top text-xs text-zinc-500">
        {user.lastLoginAt ? (
          new Date(user.lastLoginAt).toLocaleDateString()
        ) : (
          // They have been provisioned and have not arrived. Worth saying as a
          // state rather than a blank, because "did my invite work" is the
          // question this screen will be asked.
          <span className="italic">Never</span>
        )}
      </td>
      <td className="py-2 align-top text-right">
        <button
          type="button"
          onClick={remove}
          disabled={isSelf || busy}
          aria-label={`Remove ${user.email}`}
          className="rounded-md p-1 text-zinc-500 transition-colors hover:bg-black/5 hover:text-red-600 disabled:opacity-40 disabled:hover:bg-transparent disabled:hover:text-zinc-500 dark:hover:bg-white/10"
        >
          <Trash2 className="h-4 w-4" />
        </button>
      </td>
    </tr>
  );
}

/** Whether `user` holds the administrator role, for callers deciding what to warn about. */
export function isAdmin(user: PlatformUser): boolean {
  return user.roles.includes(PLATFORM_ADMIN);
}
