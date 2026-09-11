"use client";

import { Pencil, Trash2 } from "lucide-react";
import { PLATFORM_ADMIN } from "@/app/auth/roles";
import { deleteUser, type PlatformUser } from "@/app/model/users";
import { useConfirm } from "@/app/components/ConfirmDialog";
import RoleBadges from "./RoleBadges";

/**
 * One person: who they are, how their provider knows them, what they hold, and
 * the two ways to act on them.
 *
 * Nothing here changes a role. The row is a reading; editing opens a dialog.
 * That is the whole point of the split — role chips in a table make granting one
 * a single click on a list somebody is scrolling.
 *
 * Their own row cannot be removed. An administrator deleting themselves is how
 * somebody locks themselves out of the section they are standing in; iam refuses
 * to remove the last administrator regardless, and this stops the attempt being
 * made by accident.
 */
export default function UserRow({
  user,
  isSelf,
  busy,
  onEdit,
  onRemoved,
  onError,
}: {
  user: PlatformUser;
  isSelf: boolean;
  busy: boolean;
  onEdit: () => void;
  onRemoved: () => void;
  onError: (message: string) => void;
}) {
  const confirm = useConfirm();

  const remove = async () => {
    const ok = await confirm({
      title: `Remove ${user.name || user.email}?`,
      body:
        "Their API keys and role grants go with them. What they authored stays, " +
        "with the attribution cleared. They can be added again, and their next " +
        "sign-in would claim the new row.",
      confirmLabel: "Remove",
      danger: true,
    });
    if (!ok) return;
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
        <RoleBadges roles={user.roles} />
      </td>
      <td className="py-2 pr-4 align-top">
        {user.subject ? (
          // The provider's own id, for the question this screen gets asked when
          // somebody cannot get in. Monospace and breakable: these are long and
          // shaped like `auth0|65f…`, and truncating one would defeat the point
          // of showing it.
          <code className="text-xs break-all text-zinc-500">{user.subject}</code>
        ) : (
          <span className="text-xs text-zinc-400 italic">Not signed in yet</span>
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
      <td className="py-2 align-top text-right whitespace-nowrap">
        <button
          type="button"
          onClick={onEdit}
          disabled={busy}
          aria-label={`Edit ${user.email}`}
          title="Edit"
          className="rounded-md p-1 text-zinc-500 transition-colors hover:bg-black/5 hover:text-zinc-800 disabled:opacity-40 dark:hover:bg-white/10 dark:hover:text-zinc-200"
        >
          <Pencil className="h-4 w-4" />
        </button>
        <button
          type="button"
          onClick={remove}
          disabled={isSelf || busy}
          aria-label={`Remove ${user.email}`}
          title={isSelf ? "You cannot remove yourself." : "Remove"}
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
