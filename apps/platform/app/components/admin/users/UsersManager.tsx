"use client";

import { useState } from "react";
import { UserPlus } from "lucide-react";
import { useRoles } from "@/app/auth/RolesContext";
import { ROLE_LABELS } from "@/app/auth/roles";
import { INPUT } from "../fields";
import AddUserModal from "./AddUserModal";
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
  const { users, roles, loading, error, replace, reload } = directory;
  const { isAdmin } = useRoles();
  const [adding, setAdding] = useState(false);
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
          onClick={() => setAdding(true)}
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
                <th className="pb-2 pr-4">Last signed in</th>
                <th className="pb-2" />
              </tr>
            </thead>
            <tbody>
              {users.map((user) => (
                <UserRow
                  key={user.id}
                  user={user}
                  roles={roles}
                  isSelf={user.id === currentUserId}
                  onChanged={replace}
                  onRemoved={reload}
                  onError={setActionError}
                />
              ))}
            </tbody>
          </table>
          <UsersPager directory={directory} />
        </>
      )}

      {adding && (
        <AddUserModal onAdded={reload} onClose={() => setAdding(false)} />
      )}
    </div>
  );
}
