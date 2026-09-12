"use client";

import { useState } from "react";
import { UserPlus } from "lucide-react";
import { useRoles } from "@/app/auth/RolesContext";
import type { PlatformUser } from "@/app/model/users";
import { ROLE_LABELS } from "@/app/auth/roles";
import { INPUT } from "../fields";
import UserModal from "./UserModal";
import UserRow from "./UserRow";
import UsersPager from "./UsersPager";
import { useUsers } from "./useUsers";

/**
 * Who may use this platform, and what each of them may do.
 *
 * The list exists because sign-in is an allowlist: after the first person, an
 * account has to be created before its owner can get in. This is where that
 * happens, and where a role is given or taken away.
 *
 * Filtering and paging are both iam's. Either done here would mean holding the
 * whole directory in the browser to answer a question about part of it, and a
 * filter applied after paging returns short pages of an unknown total.
 */
export default function UsersManager({ currentUserId }: { currentUserId: string }) {
  const directory = useUsers();
  const { users, roles, loading, error, reload } = directory;
  const { isAdmin } = useRoles();
  // Which dialog is open: none, adding (null), or editing a person. One piece of
  // state rather than two booleans, because the two can never both be true and
  // two flags would let them.
  const [editing, setEditing] = useState<PlatformUser | null | undefined>(undefined);
  const [actionError, setActionError] = useState<string | null>(null);

  // Belt and braces: the section's layout and every action behind it already
  // require the role, and this is only so a stale page says something honest
  // rather than showing controls whose every use would be refused.
  if (!isAdmin) {
    return <p className="p-6 text-sm text-zinc-500">Administrators only.</p>;
  }

  return (
    <div className="flex flex-col gap-4 p-6">
      <div className="flex flex-wrap items-end gap-3">
        <input
          className={INPUT + " min-w-56"}
          value={directory.query}
          onChange={(e) => directory.setQuery(e.target.value)}
          placeholder="Filter by name or email"
          aria-label="Filter by name or email"
        />
        <select
          className={INPUT}
          value={directory.role}
          onChange={(e) => directory.setRole(e.target.value)}
          aria-label="Filter by role"
        >
          <option value="">Any role</option>
          {roles.map(({ role }) => (
            <option key={role} value={role}>
              {ROLE_LABELS[role] ?? role}
            </option>
          ))}
        </select>
        <button
          type="button"
          onClick={() => setEditing(null)}
          className="ml-auto inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white transition-colors hover:bg-sky-500"
        >
          <UserPlus size={15} />
          Add a person
        </button>
      </div>

      {error && <p className="text-sm text-red-600 dark:text-red-400">{error}</p>}
      {actionError && (
        <p className="text-sm text-red-600 dark:text-red-400">{actionError}</p>
      )}

      {loading ? (
        <p className="text-sm text-zinc-500">Loading…</p>
      ) : users.length === 0 ? (
        <p className="text-sm text-zinc-500">
          {directory.query || directory.role
            ? "Nobody matches that filter."
            : "Nobody yet."}
        </p>
      ) : (
        <>
          <table className="w-full text-left">
            <thead>
              <tr className="text-xs font-medium text-zinc-500">
                <th className="pb-2 pr-4">Person</th>
                <th className="pb-2 pr-4">Roles</th>
                <th className="pb-2 pr-4">OIDC subject</th>
                <th className="pb-2 pr-4">Last signed in</th>
                <th className="pb-2" />
              </tr>
            </thead>
            <tbody>
              {users.map((user) => (
                <UserRow
                  key={user.id}
                  user={user}
                  isSelf={user.id === currentUserId}
                  busy={loading}
                  onEdit={() => setEditing(user)}
                  onRemoved={reload}
                  onError={setActionError}
                />
              ))}
            </tbody>
          </table>
          <UsersPager directory={directory} />
        </>
      )}

      {editing !== undefined && (
        <UserModal
          person={editing ?? undefined}
          catalogue={roles}
          isSelf={editing?.id === currentUserId}
          onSaved={reload}
          onClose={() => setEditing(undefined)}
        />
      )}
    </div>
  );
}
