import path from "node:path";

/**
 * What a terminal session starts: which shell, with which arguments and environment,
 * and the startup files that put the editor's binaries first on PATH. Pure, so the
 * rules can be tested without Electron or a pty.
 *
 * Prepending to PATH before the shell starts is not enough. A login shell reads
 * /etc/zprofile, whose path_helper moves the system directories ahead of what it
 * inherited, and then the user's profile, where `brew shellenv` and the like prepend
 * their own. Any `octo` installed there would win. So zsh and bash are started on
 * startup files of ours that read the user's own and only then prepend, as VS Code's
 * shell integration does. Other shells get the inherited prepend alone.
 */

/**
 * Electron's switches for its own processes. ELECTRON_RUN_AS_NODE above all: an
 * Electron-built tool run from the terminal would come up as plain Node.
 */
const WITHHELD = ["ELECTRON_RUN_AS_NODE", "ELECTRON_NO_ATTACH_CONSOLE"];

/** Carries the binaries' directory to our startup files, which prepend it last. */
const BIN_VAR = "OCTO_TERM_BIN";
/** The user's own ZDOTDIR, which our zsh files read from and hand back. */
const USER_ZDOTDIR_VAR = "OCTO_USER_ZDOTDIR";

export interface Launch {
  file: string;
  args: string[];
  env: Record<string, string>;
}

export interface LaunchOptions {
  /** Where `octo` and `dolphin` link to the binaries the editor runs. */
  shimDir: string;
  /** Where {@link startupFiles} were written. */
  startupDir: string;
  /** The app's version, for TERM_PROGRAM_VERSION. */
  version: string;
}

/** The name a session is listed under. */
export function shellTitle(file: string): string {
  return path.basename(file);
}

/** How to start the user's shell so it ends up with the editor's binaries first. */
export function launch(base: NodeJS.ProcessEnv, opts: LaunchOptions): Launch {
  const file = base.SHELL || "/bin/zsh";
  const env = terminalEnv(base, opts);
  const shell = shellTitle(file);

  if (shell === "zsh") {
    // zsh reads every startup file from ZDOTDIR, so pointing it at ours runs ours in
    // place of each of the user's, and ours read the user's back in.
    env[USER_ZDOTDIR_VAR] = base.ZDOTDIR || base.HOME || "";
    env.ZDOTDIR = path.join(opts.startupDir, "zsh");
    return { file, args: ["-l"], env };
  }
  if (shell === "bash") {
    // A login bash ignores --rcfile, so this is an interactive one whose rc file does
    // what a login would, then prepends.
    return { file, args: ["--rcfile", path.join(opts.startupDir, "bash", "rc"), "-i"], env };
  }
  // A login shell, as Terminal.app starts one: an app opened from the Finder inherits
  // launchd's bare PATH, and only the user's profile puts the rest back.
  return { file, args: ["-l"], env };
}

/**
 * The session's environment: this process's, with the binaries first on PATH for the
 * shells we cannot start on files of our own, and the TERM_* variables a terminal
 * emulator sets.
 */
function terminalEnv(base: NodeJS.ProcessEnv, opts: LaunchOptions): Record<string, string> {
  const ret: Record<string, string> = {};
  for (const [key, value] of Object.entries(base)) {
    if (value !== undefined && !WITHHELD.includes(key)) ret[key] = value;
  }
  ret.PATH = [opts.shimDir, base.PATH].filter(Boolean).join(path.delimiter);
  ret[BIN_VAR] = opts.shimDir;
  ret.TERM = "xterm-256color";
  ret.COLORTERM = "truecolor";
  ret.TERM_PROGRAM = "Octo";
  ret.TERM_PROGRAM_VERSION = opts.version;
  // A Finder launch has no locale, and without one zsh mangles anything past ASCII.
  if (!ret.LANG) ret.LANG = "en_US.UTF-8";
  return ret;
}

/** Single-quoted for sh and zsh, so a path with spaces or quotes survives. */
function quote(value: string): string {
  return `'${value.replaceAll("'", `'\\''`)}'`;
}

/**
 * The startup files {@link launch} points zsh and bash at, keyed by their path under
 * `startupDir`.
 */
export function startupFiles(startupDir: string): Record<string, string> {
  const ours = quote(path.join(startupDir, "zsh"));

  // Each reads the user's file of the same name with ZDOTDIR set to theirs, so what
  // the file derives from ZDOTDIR — HISTFILE, a plugin manager's paths — is theirs.
  // A user .zshenv may move ZDOTDIR itself; the files after it read from there.
  const zsh = (name: string, last = "") =>
    [
      `ZDOTDIR="$${USER_ZDOTDIR_VAR}"`,
      `[[ -f "$ZDOTDIR/${name}" ]] && source "$ZDOTDIR/${name}"`,
      `${USER_ZDOTDIR_VAR}="$ZDOTDIR"`,
      last || `ZDOTDIR=${ours}`,
      "",
    ].join("\n");

  // .zlogin is the last file a login shell reads: hand ZDOTDIR back for good, and only
  // now, after every profile has had its say, put the editor's binaries first.
  const finish = [
    `export PATH="$${BIN_VAR}:$PATH"`,
    `unset ${USER_ZDOTDIR_VAR} ${BIN_VAR}`,
  ].join("\n");

  const bash = [
    "[ -f /etc/profile ] && . /etc/profile",
    'if [ -f "$HOME/.bash_profile" ]; then . "$HOME/.bash_profile"',
    'elif [ -f "$HOME/.bash_login" ]; then . "$HOME/.bash_login"',
    'elif [ -f "$HOME/.profile" ]; then . "$HOME/.profile"',
    "fi",
    `export PATH="$${BIN_VAR}:$PATH"`,
    `unset ${BIN_VAR}`,
    "",
  ].join("\n");

  return {
    "zsh/.zshenv": zsh(".zshenv"),
    "zsh/.zprofile": zsh(".zprofile"),
    "zsh/.zshrc": zsh(".zshrc"),
    "zsh/.zlogin": zsh(".zlogin", finish),
    "bash/rc": bash,
  };
}
