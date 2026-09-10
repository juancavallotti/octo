"use client";

import { useMemo, useState } from "react";
import { useRoles } from "@/app/auth/RolesContext";
import { ROLE_LABELS } from "@/app/auth/roles";
import { INPUT } from "../fields";
import AddUserForm from "./AddUserForm";
import UserRow from "./UserRow";
import { useUsers } from "./useUsers";

/**
 * Who may use this platform, and what each of them may do.
 *
 * The list exists because sign-in is an allowlist: after the first person, an
 * account has to be created before its owner can get in. This is where that
 * happens, and where a role is given or taken away.
 *
 * Filtering is done here in the browser rather than by iam. An installation's
 * user list is tens of rows — it is a company, not a data set — so paging it
 * would be machinery in front of a list that fits on a screen.
 */
export default function UsersManager({ currentUserId }: { currentUserId?: string }) {
  const { users, roles, error, reload, replace } = useUsers();
  const { isAdmin } = useRoles();
  const [query, setQuery] = useState("");
  const [roleFilter, setRoleFilter] = useState("");
  const [actionError, setActionError] = useState<string | null>(null);

  const shown = useMemo(() => {
    if (!users) return null;
    const needle = query.trim().toLowerCase();
    return users.filter((u) => {
      const matchesText =
        !needle ||
        u.email.toLowerCase().includes(needle) ||
        u.name.toLowerCase().includes(needle);
      return matchesText && (!roleFilter || u.roles.includes(roleFilter));
    });
  }, [users, query, roleFilter]);

  // Belt and braces: the section's layout and every action behind it already
  // require the role, and this is only so a stale page says something honest
  // rather than showing controls whose every use would be refused.
  if (!isAdmin) {
    return <p className="p-6 text-sm text-zinc-500">Administrators only.</p>;
  }

  return (
    <div className="flex flex-col gap-4 p-6">
      <AddUserForm onAdded={reload} />

      <div className="flex flex-wrap items-end gap-3">
        <input
          className={INPUT + " min-w-56"}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Filter by name or email"
          aria-label="Filter by name or email"
        />
        <select
          className={INPUT}
          value={roleFilter}
          onChange={(e) => setRoleFilter(e.target.value)}
          aria-label="Filter by role"
        >
          <option value="">Any role</option>
          {roles.map(({ role }) => (
            <option key={role} value={role}>
              {ROLE_LABELS[role] ?? role}
            </option>
          ))}
        </select>
      </div>

      {error && <p className="text-sm text-red-600 dark:text-red-400">{error}</p>}
      {actionError && (
        <p className="text-sm text-red-600 dark:text-red-400">{actionError}</p>
      )}

      {shown === null ? (
        <p className="text-sm text-zinc-500">Loading…</p>
      ) : shown.length === 0 ? (
        <p className="text-sm text-zinc-500">
          {users?.length ? "Nobody matches that filter." : "Nobody yet."}
        </p>
      ) : (
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
            {shown.map((user) => (
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
      )}
    </div>
  );
}
