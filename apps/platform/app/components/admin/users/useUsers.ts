"use client";

import { useCallback, useEffect, useState } from "react";
import {
  listRoles,
  listUsers,
  type PlatformUser,
  type RoleOption,
} from "@/app/model/users";

/**
 * The people on this platform and the roles they can hold.
 *
 * Both are loaded together because the list is unreadable without the catalogue:
 * a row shows every role with the ones it holds turned on, so a role nobody has
 * yet still has to be offered.
 *
 * `null` means not loaded, which is what tells the manager to say nothing rather
 * than to say "no users" while it is still asking.
 */
export interface UsersData {
  users: PlatformUser[] | null;
  roles: RoleOption[];
  error: string | null;
  reload: () => Promise<void>;
  /** Replace one row in place, for a change that answered with the new user. */
  replace: (user: PlatformUser) => void;
}

export function useUsers(): UsersData {
  const [users, setUsers] = useState<PlatformUser[] | null>(null);
  const [roles, setRoles] = useState<RoleOption[]>([]);
  const [error, setError] = useState<string | null>(null);

  // A promise chain rather than an async body, so nothing sets state in the
  // synchronous part of the effect below — the form the other managers use.
  const reload = useCallback(
    () =>
      Promise.all([listUsers(), listRoles()]).then(
        ([people, catalogue]) => {
          setUsers(people);
          setRoles(catalogue);
          setError(null);
        },
        (e) => setError((e as Error).message),
      ),
    [],
  );

  useEffect(() => {
    reload();
  }, [reload]);

  // Grants and profile edits answer with the whole user, so the row can be
  // swapped rather than the list re-fetched — which keeps a filtered view from
  // jumping under somebody's hands mid-edit.
  const replace = useCallback((user: PlatformUser) => {
    setUsers((current) =>
      current ? current.map((u) => (u.id === user.id ? user : u)) : current,
    );
  }, []);

  return { users, roles, error, reload, replace };
}
