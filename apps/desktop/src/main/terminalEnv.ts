import path from "node:path";

/**
 * What a terminal session starts: which shell, and the environment it starts with.
 * Pure, so the rules can be tested without Electron or a pty.
 */

/**
 * Variables this process carries for its own children that must not reach a user's
 * shell. ELECTRON_RUN_AS_NODE above all: an `electron` or Electron-built tool run from
 * the terminal would come up as plain Node.
 */
const WITHHELD = [
  "ELECTRON_RUN_AS_NODE",
  "ELECTRON_NO_ATTACH_CONSOLE",
  "OCTO_FS_DIR",
  "OCTO_BIN_PATH",
  "DOLPHIN_BIN_PATH",
  "OCTO_RUN_DIR",
  "OCTO_DESKTOP_PORT",
  "PORT",
  "HOSTNAME",
  "NODE_ENV",
  "NEXT_TELEMETRY_DISABLED",
];

/** The user's login shell, as Terminal.app would start it. */
export function shellFor(env: NodeJS.ProcessEnv): { file: string; args: string[] } {
  const file = env.SHELL || "/bin/zsh";
  // A login shell, as VS Code starts one on macOS: an app opened from the Finder
  // inherits launchd's bare PATH, and only the user's profile puts Homebrew and the
  // rest back.
  return { file, args: ["-l"] };
}

/** The name a session is listed under before it names itself. */
export function shellTitle(file: string): string {
  return path.basename(file);
}

/**
 * The session's environment: this process's, with `shimDir` first on PATH so `octo`
 * and `dolphin` are the binaries the editor runs, and the TERM_* variables a terminal
 * emulator sets.
 */
export function terminalEnv(
  base: NodeJS.ProcessEnv,
  shimDir: string,
  version: string,
): Record<string, string> {
  const ret: Record<string, string> = {};
  for (const [key, value] of Object.entries(base)) {
    if (value !== undefined && !WITHHELD.includes(key)) ret[key] = value;
  }
  ret.PATH = [shimDir, base.PATH].filter(Boolean).join(path.delimiter);
  ret.TERM = "xterm-256color";
  ret.COLORTERM = "truecolor";
  ret.TERM_PROGRAM = "Octo";
  ret.TERM_PROGRAM_VERSION = version;
  // A Finder launch has no locale, and without one zsh mangles anything past ASCII.
  if (!ret.LANG) ret.LANG = "en_US.UTF-8";
  return ret;
}
