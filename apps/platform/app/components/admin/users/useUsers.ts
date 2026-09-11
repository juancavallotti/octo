"use client";

import { useCallback, useEffect, useState } from "react";
import {
  listRoles,
  listUsers,
  type PlatformUser,
  type RoleOption,
} from "@/app/model/users";

/**
 * One page of the people on this platform, and the roles they can hold.
 *
 * Both are loaded together because the list is unreadable without the
 * catalogue: a row shows every role with the ones it holds turned on, so a role
 * nobody has yet still has to be offered. The catalogue is four constants and is
 * fetched once; the page is re-fetched whenever the filter or the position
 * changes.
 *
 * Paging is the server's. The cursors already handed out are kept here so
 * "previous" is a step back through them rather than a second kind of query —
 * keyset paging has no notion of going backwards, and the pages already visited
 * are exactly the answer.
 *
 * `loading` is tracked separately from `users` rather than inferred from it
 * being empty. Inferring it means a first load that fails stays "Loading…"
 * forever, underneath the error explaining why it never will.
 */
export interface UsersData {
  users: PlatformUser[];
  roles: RoleOption[];
  loading: boolean;
  error: string | null;
  /** The substring the listing is filtered by. */
  query: string;
  setQuery: (next: string) => void;
  /** The role the listing is narrowed to, or "" for everybody. */
  role: string;
  setRole: (next: string) => void;
  /** Zero-based, for saying which page is on screen. */
  page: number;
  hasPrevious: boolean;
  hasNext: boolean;
  next: () => void;
  previous: () => void;
  /** Re-read the page on screen — after an add, or a removal. */
  reload: () => void;
  /** Replace one row in place, for a change that answered with the new user. */
  replace: (user: PlatformUser) => void;
}

/** How many rows a page holds. A screenful, and the same number iam defaults to. */
const PAGE_SIZE = 25;

export function useUsers(): UsersData {
  const [roles, setRoles] = useState<RoleOption[]>([]);
  // Two requests, two errors. Sharing one means a catalogue that failed to load
  // is forgotten the moment a page of people arrives — leaving a screen with no
  // role controls and nothing saying why.
  const [rolesError, setRolesError] = useState<string | null>(null);
  const [pageError, setPageError] = useState<string | null>(null);
  const [query, setQueryState] = useState("");
  const [role, setRoleState] = useState("");
  // The cursor for each page, first entry "" for the first page. Its length is
  // how many pages have been visited; `page` indexes into it.
  const [cursors, setCursors] = useState<string[]>([""]);
  const [page, setPage] = useState(0);
  // Bumped to re-read the same filter and position — after an add or a removal.
  const [generation, setGeneration] = useState(0);

  // What was asked for, as one value. The answer below carries the key it
  // belongs to, which is what makes "still loading" a comparison rather than a
  // flag somebody has to remember to clear.
  const wanted = JSON.stringify([query, role, page, generation]);
  const [answer, setAnswer] = useState<{
    key: string;
    items: PlatformUser[];
    nextCursor?: string;
  } | null>(null);

  // The catalogue, once: it is a list of constants and does not change while
  // somebody is reading the directory.
  useEffect(() => {
    listRoles().then(setRoles, (e) => setRolesError((e as Error).message));
  }, []);

  useEffect(() => {
    let current = true;
    listUsers({ q: query, role, limit: PAGE_SIZE, cursor: cursors[page] }).then(
      (result) => {
        // Typing in the filter fires one request per keystroke; without this an
        // earlier, slower reply can land last and show the wrong page.
        if (!current) return;
        // A page emptied by a removal is a page that no longer exists. Stepping
        // back is the only honest answer: staying puts "nobody here" in front of
        // somebody looking at a directory that has people in it.
        if (result.items.length === 0 && page > 0) {
          setPage((p) => p - 1);
          return;
        }
        setAnswer({ key: wanted, items: result.items, nextCursor: result.nextCursor });
        setPageError(null);
      },
      (e) => {
        if (!current) return;
        setPageError((e as Error).message);
        setAnswer({ key: wanted, items: [] });
      },
    );
    return () => {
      current = false;
    };
  }, [query, role, cursors, page, wanted]);

  const error = pageError ?? rolesError;
  const loading = answer?.key !== wanted;
  const users = answer?.items ?? [];
  const nextCursor = answer?.nextCursor;

  // A new filter is a new listing: the cursors collected for the old one name
  // positions in a sequence that no longer exists.
  const restart = useCallback(() => {
    setCursors([""]);
    setPage(0);
  }, []);

  const setQuery = useCallback(
    (next: string) => {
      setQueryState(next);
      restart();
    },
    [restart],
  );

  const setRole = useCallback(
    (next: string) => {
      setRoleState(next);
      restart();
    },
    [restart],
  );

  const next = useCallback(() => {
    if (!nextCursor) return;
    // The cursor just answered for is the one to keep, and everything past it is
    // discarded: a page boundary moves when somebody is added or removed, so a
    // cursor collected before that would resume from a row that is no longer
    // where it was.
    setCursors((current) => [...current.slice(0, page + 1), nextCursor]);
    setPage((p) => p + 1);
  }, [nextCursor, page]);

  const previous = useCallback(() => setPage((p) => Math.max(0, p - 1)), []);

  const reload = useCallback(() => setGeneration((g) => g + 1), []);

  // Grants and profile edits answer with the whole user, so the row can be
  // swapped rather than the page re-fetched — which keeps the list from jumping
  // under somebody's hands mid-edit.
  const replace = useCallback((user: PlatformUser) => {
    setAnswer((current) =>
      current === null
        ? current
        : {
            ...current,
            items: current.items.map((u) => (u.id === user.id ? user : u)),
          },
    );
  }, []);

  return {
    users,
    roles,
    loading,
    error,
    query,
    setQuery,
    role,
    setRole,
    page,
    hasPrevious: page > 0,
    hasNext: Boolean(nextCursor),
    next,
    previous,
    reload,
    replace,
  };
}
