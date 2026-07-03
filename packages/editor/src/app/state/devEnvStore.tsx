"use client";

import { createContext, useContext, type ReactNode } from "react";

/**
 * The editor's dev-env capability: reads and writes the *values* of a run's
 * declared environment variables — the credentials that satisfy the document's
 * `${NAME}` variables when a local runner is spawned. These are stored as the
 * integration's `.env.dev` resource in the host's backend (the standalone app's
 * flows dir, the platform's orchestrator DB), NOT in the browser — so the
 * credentials never live client-side and an MCP run can read them from the store.
 * The store moves the raw `.env.dev` content; the Dev .env panel parses it into
 * per-variable values with the dotenv helpers here.
 *
 * Editor components read it through {@link useDevEnvStore}; when it is null the
 * Dev .env panel shows nothing to edit.
 */

/** Map of declared env var name → its dev value. */
export type DevEnvMap = Record<string, string>;

/** What the editor needs to load and persist a run's dev-env values. */
export interface DevEnvStore {
  /** Load the integration's `.env.dev` content (empty string when none). */
  load(integrationId: string | null): Promise<string>;
  /** Persist the integration's `.env.dev` content. */
  save(integrationId: string | null, content: string): Promise<void>;
  /**
   * Whether dev env can be edited for the given integration. The platform can't
   * persist a resource for an unsaved draft (no id yet); standalone shares a
   * single file, so it always can.
   */
  canEdit(integrationId: string | null): boolean;
}

const DevEnvStoreContext = createContext<DevEnvStore | null>(null);

export function DevEnvStoreProvider({
  value,
  children,
}: {
  value: DevEnvStore | null;
  children: ReactNode;
}) {
  return (
    <DevEnvStoreContext.Provider value={value}>
      {children}
    </DevEnvStoreContext.Provider>
  );
}

/** The current dev-env store, or null when none is provided. */
export function useDevEnvStore(): DevEnvStore | null {
  return useContext(DevEnvStoreContext);
}

/**
 * Parse `.env.dev` content into a name→value map, matching the runtime's dotenv
 * rules (runtime/core/internal/dsl/dotenv.go): each non-blank, non-`#` line is a
 * `KEY=VALUE` assignment, an optional leading `export ` is dropped, key and value
 * are trimmed, and one pair of matching surrounding quotes is stripped. Malformed
 * lines (no `=`) are skipped rather than thrown — the editor is lenient about
 * hand-edited files; the runtime is the strict judge at load.
 */
export function parseDotEnv(content: string): DevEnvMap {
  const out: DevEnvMap = {};
  for (const line of content.split(/\r?\n/)) {
    const raw = line.trim();
    if (raw === "" || raw.startsWith("#")) continue;
    const body = raw.startsWith("export ") ? raw.slice("export ".length) : raw;
    const eq = body.indexOf("=");
    if (eq === -1) continue;
    const key = body.slice(0, eq).trim();
    if (key === "") continue;
    out[key] = unquote(body.slice(eq + 1).trim());
  }
  return out;
}

/** Strip one pair of matching surrounding single or double quotes. */
function unquote(value: string): string {
  if (value.length >= 2) {
    const first = value[0];
    if ((first === '"' || first === "'") && value[value.length - 1] === first) {
      return value.slice(1, -1);
    }
  }
  return value;
}

/**
 * Serialize a name→value map to `.env.dev` content. A value is double-quoted when
 * it would otherwise not round-trip through the runtime's trim/unquote — leading
 * or trailing whitespace, a leading `#`, or an already-quote-wrapped value.
 * Entries are emitted in sorted key order for a stable file.
 */
export function serializeDotEnv(map: DevEnvMap): string {
  const keys = Object.keys(map).sort();
  return keys.map((k) => `${k}=${quoteIfNeeded(map[k])}`).join("\n") + (keys.length ? "\n" : "");
}

function quoteIfNeeded(value: string): string {
  const needs =
    value !== value.trim() ||
    value.startsWith("#") ||
    (value.length >= 2 &&
      (value[0] === '"' || value[0] === "'") &&
      value[value.length - 1] === value[0]);
  return needs ? `"${value}"` : value;
}
