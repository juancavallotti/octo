import { existsSync, mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import path from "node:path";

/**
 * The shell's memory between launches: which folder was open, which ones came
 * before, and any port the user pinned.
 *
 * Hand-rolled rather than electron-store, because the whole feature is "read a
 * small JSON file, write it back atomically" and a dependency for that is a
 * dependency to keep, audit and package. Atomic because the alternative — a
 * truncating write interrupted by a quit — loses the user's recents list at
 * exactly the moment they would notice.
 */

export interface Vault {
  path: string;
  openedAt: number;
}

/**
 * What the user chose in Settings, as opposed to what the app remembers on its own.
 *
 * `runtime` holds *paths*, not versions, and deliberately: the bundled binaries are
 * the default and an override is a file the user pointed at. A future "download a
 * version" source would fill these same fields in with a path under userData, so it
 * needs no migration and no second notion of which runtime is in use.
 */
export interface DesktopSettings {
  /** Overrides for the runtime binaries. Absent means the ones inside the app. */
  runtime?: { octo?: string; dolphin?: string };
  /** Check for a new version of Octo Desktop on launch. Absent means yes. */
  autoUpdateCheck?: boolean;
}

export interface DesktopState {
  lastVault?: string;
  recents: Vault[];
  port?: number;
  window?: { x?: number; y?: number; width: number; height: number };
  settings?: DesktopSettings;
}

/** How many folders to remember. Long enough to be useful, short enough to scan. */
export const MAX_RECENTS = 10;

export const EMPTY: DesktopState = { recents: [] };

export function stateFile(dir: string): string {
  return path.join(dir, "state.json");
}

/**
 * Read the stored state, or the empty state.
 *
 * Any failure — missing, unreadable, truncated, or valid JSON of the wrong shape
 * — resolves to the empty state rather than an error. This file is a convenience;
 * losing it costs the user a folder picker they would otherwise have skipped, and
 * refusing to launch over it would be wildly out of proportion.
 */
export function read(dir: string): DesktopState {
  try {
    const raw = JSON.parse(readFileSync(stateFile(dir), "utf8")) as unknown;
    if (!raw || typeof raw !== "object") return { ...EMPTY };
    const state = raw as Partial<DesktopState>;
    return {
      lastVault: typeof state.lastVault === "string" ? state.lastVault : undefined,
      recents: Array.isArray(state.recents)
        ? state.recents.filter(
            (v): v is Vault =>
              !!v && typeof v.path === "string" && typeof v.openedAt === "number",
          )
        : [],
      port: typeof state.port === "number" ? state.port : undefined,
      // Validated like everything else here rather than passed through: this file
      // is user-editable and survives upgrades, so a `window` of the wrong shape
      // reaches BrowserWindow as NaN or a string and throws at construction —
      // which is a launch failure caused by a remembered convenience.
      window: validWindow(state.window),
      settings: validSettings(state.settings),
    };
  } catch {
    return { ...EMPTY };
  }
}

/**
 * Settings, with every field checked.
 *
 * A path of the wrong type would reach spawn() as a non-string and fail the launch,
 * and this file is user-editable — so a hand-edited mistake has to cost the setting,
 * not the app. Whether the path *exists* is not checked here: the file is read at
 * launch and the binary may live on a volume that is not mounted yet, so that
 * question belongs to the moment the binary is used.
 */
function validSettings(s: DesktopSettings | undefined): DesktopSettings | undefined {
  if (!s || typeof s !== "object") return undefined;
  const str = (v: unknown) => (typeof v === "string" && v !== "" ? v : undefined);
  const runtime = s.runtime && typeof s.runtime === "object" ? s.runtime : undefined;
  const octo = str(runtime?.octo);
  const dolphin = str(runtime?.dolphin);
  return {
    ...(octo || dolphin ? { runtime: { ...(octo ? { octo } : {}), ...(dolphin ? { dolphin } : {}) } } : {}),
    ...(typeof s.autoUpdateCheck === "boolean" ? { autoUpdateCheck: s.autoUpdateCheck } : {}),
  };
}

/** Remembered window geometry, or undefined if it is not a usable rectangle. */
function validWindow(w: DesktopState["window"]): DesktopState["window"] {
  if (!w || typeof w !== "object") return undefined;
  const finite = (n: unknown): n is number => typeof n === "number" && Number.isFinite(n);
  if (!finite(w.width) || !finite(w.height) || w.width <= 0 || w.height <= 0) return undefined;
  const optional = (n: unknown) => n === undefined || finite(n);
  if (!optional(w.x) || !optional(w.y)) return undefined;
  return w;
}

/** Write the state, atomically: full file to a temp name, then one rename. */
export function write(dir: string, state: DesktopState): void {
  mkdirSync(dir, { recursive: true });
  const target = stateFile(dir);
  const tmp = `${target}.tmp`;
  writeFileSync(tmp, `${JSON.stringify(state, null, 2)}\n`, "utf8");
  renameSync(tmp, target);
}

/**
 * Record a folder as the current one: most recent first, no duplicates, capped.
 * Pure, so the ordering rules are testable without touching a disk.
 */
export function remember(state: DesktopState, vaultPath: string, now = Date.now()): DesktopState {
  const others = state.recents.filter((v) => v.path !== vaultPath);
  return {
    ...state,
    lastVault: vaultPath,
    recents: [{ path: vaultPath, openedAt: now }, ...others].slice(0, MAX_RECENTS),
  };
}

/**
 * Drop remembered folders that are no longer there.
 *
 * A folder can be renamed, moved or unmounted between launches, and a menu that
 * offers one is a menu with a dead entry. Deliberately applied on read for the
 * menu rather than written back: an external drive that is merely unplugged today
 * should still be in the list when it is plugged back in.
 */
export function existing(recents: Vault[]): Vault[] {
  return recents.filter((v) => existsSync(v.path));
}
