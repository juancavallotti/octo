"use client";

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
  const toggle = async (role: string, grant: boolean) => {
    try {
      onChanged(await (grant ? grantRole(user.id, role) : revokeRole(user.id, role)));
    } catch (e) {
      onError((e as Error).message);
    }
  };

  const remove = async () => {
    try {
      await deleteUser(user.id);
      onRemoved();
    } catch (e) {
      onError((e as Error).message);
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
          disabled={isSelf}
          disabledReason="You cannot change your own roles."
          onToggle={toggle}
        />
      </td>
      <td className="py-2 pr-4 align-top text-xs text-zinc-500">
        {user.lastLoginAt ? new Date(user.lastLoginAt).toLocaleDateString() : "never"}
      </td>
      <td className="py-2 align-top text-right">
        <button
          type="button"
          onClick={remove}
          disabled={isSelf}
          title={isSelf ? "You cannot remove yourself." : `Remove ${user.email}`}
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
