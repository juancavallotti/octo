import { execFileSync } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterAll, describe, expect, it } from "vitest";
import { launch, startupFiles } from "./terminalEnv";

const opts = { shimDir: "/shim", startupDir: "/startup", version: "1.2.3" };

describe("launch", () => {
  it("starts zsh on our startup files, remembering the user's ZDOTDIR", () => {
    const l = launch({ SHELL: "/bin/zsh", HOME: "/Users/me", ZDOTDIR: "/Users/me/.config/zsh" }, opts);
    expect(l.args).toEqual(["-l"]);
    expect(l.env.ZDOTDIR).toBe("/startup/zsh");
    expect(l.env.OCTO_USER_ZDOTDIR).toBe("/Users/me/.config/zsh");
  });

  it("starts bash interactively on our rc file, since a login bash ignores one", () => {
    const l = launch({ SHELL: "/opt/homebrew/bin/bash" }, opts);
    expect(l.args).toEqual(["--rcfile", "/startup/bash/rc", "-i"]);
  });

  it("starts any other shell as a login shell, with the binaries prepended", () => {
    const l = launch({ SHELL: "/opt/homebrew/bin/fish", PATH: "/usr/bin" }, opts);
    expect(l.args).toEqual(["-l"]);
    expect(l.env.PATH).toBe("/shim:/usr/bin");
  });

  it("falls back to zsh when SHELL is unset", () => {
    expect(launch({}, opts).file).toBe("/bin/zsh");
  });

  it("withholds Electron's switches and keeps the user's own variables", () => {
    const { env } = launch(
      { ELECTRON_RUN_AS_NODE: "1", NODE_ENV: "development", PORT: "3000", HOME: "/h" },
      opts,
    );
    expect(env).not.toHaveProperty("ELECTRON_RUN_AS_NODE");
    expect(env).toMatchObject({ NODE_ENV: "development", PORT: "3000", HOME: "/h" });
  });

  it("identifies itself as a 256-colour terminal with a UTF-8 locale", () => {
    expect(launch({}, opts).env).toMatchObject({
      TERM: "xterm-256color",
      COLORTERM: "truecolor",
      TERM_PROGRAM: "Octo",
      TERM_PROGRAM_VERSION: "1.2.3",
      LANG: "en_US.UTF-8",
    });
  });
});

// The point of the startup files: a profile that prepends its own `octo`, as
// `brew shellenv` does, must not win over the editor's.
describe("startup files", () => {
  const root = mkdtempSync(path.join(tmpdir(), "octo term '"));
  afterAll(() => rmSync(root, { recursive: true, force: true }));

  const home = path.join(root, "home");
  const shim = path.join(root, "shim");
  const brew = path.join(root, "brew");
  const startup = path.join(root, "startup");
  for (const dir of [home, shim, brew]) mkdirSync(dir, { recursive: true });
  for (const [dir, label] of [
    [shim, "editor"],
    [brew, "brew"],
  ]) {
    writeFileSync(path.join(dir, "octo"), `#!/bin/sh\necho ${label}\n`);
    chmodSync(path.join(dir, "octo"), 0o755);
  }
  writeFileSync(path.join(home, ".zprofile"), `export PATH='${brew}':"$PATH"\n`);
  writeFileSync(path.join(home, ".zshrc"), "READ_ZSHRC=1\n");
  writeFileSync(path.join(home, ".bash_profile"), `export PATH='${brew}':"$PATH"\n`);
  for (const [file, content] of Object.entries(startupFiles(startup))) {
    mkdirSync(path.dirname(path.join(startup, file)), { recursive: true });
    writeFileSync(path.join(startup, file), content);
  }

  // The octo found, whether .zshrc ran, ZDOTDIR, any of our variables left over, how
  // many times the shim is on PATH, and whether history would be written among ours.
  const probe = [
    'echo "$(octo) ${READ_ZSHRC:-} ${ZDOTDIR:-} ${OCTO_TERM_BIN:-}${OCTO_USER_ZDOTDIR:-}',
    `$(echo "$PATH" | tr : '\\n' | grep -cxF "$PROBE_SHIM")`,
    '$(case "${HISTFILE:-}" in ("$PROBE_STARTUP"/*) echo ours;; (*) echo theirs;; esac)"',
  ].join(" ");
  const run = (shell: string, extra: string[]) => {
    const l = launch({ SHELL: shell, HOME: home, PATH: "/usr/bin:/bin" }, { ...opts, shimDir: shim, startupDir: startup });
    return execFileSync(l.file, [...l.args, ...extra, "-c", probe], {
      // The paths reach the probe as variables: the root has a quote in its name.
      env: { ...l.env, PROBE_SHIM: shim, PROBE_STARTUP: startup },
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    }).trim();
  };

  it.skipIf(!existsSync("/bin/zsh"))("put the editor's octo first in zsh, after the user's files", () => {
    // Interactive, as in a pty, so .zshrc is read; ZDOTDIR is handed back to the user.
    expect(run("/bin/zsh", ["-i"])).toBe(`editor 1 ${home}  1 theirs`);
  });

  it.skipIf(!existsSync("/bin/bash"))("put the editor's octo first in bash, after the profile", () => {
    expect(run("/bin/bash", [])).toBe("editor    1 theirs");
  });
});
