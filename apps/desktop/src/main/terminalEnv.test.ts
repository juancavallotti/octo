import { describe, expect, it } from "vitest";
import { shellFor, terminalEnv } from "./terminalEnv";

describe("shellFor", () => {
  it("starts the user's shell as a login shell", () => {
    expect(shellFor({ SHELL: "/opt/homebrew/bin/fish" })).toEqual({
      file: "/opt/homebrew/bin/fish",
      args: ["-l"],
    });
  });

  it("falls back to zsh when SHELL is unset", () => {
    expect(shellFor({}).file).toBe("/bin/zsh");
  });
});

describe("terminalEnv", () => {
  const base = {
    PATH: "/usr/bin:/bin",
    HOME: "/Users/me",
    ELECTRON_RUN_AS_NODE: "1",
    OCTO_FS_DIR: "/Users/me/Octo",
    PORT: "8477",
    LANG: "fr_FR.UTF-8",
  };

  it("puts the shim directory first on PATH", () => {
    expect(terminalEnv(base, "/shim", "1.0.0").PATH).toBe("/shim:/usr/bin:/bin");
  });

  it("keeps the user's own variables and withholds the app's", () => {
    const env = terminalEnv(base, "/shim", "1.0.0");
    expect(env.HOME).toBe("/Users/me");
    expect(env.LANG).toBe("fr_FR.UTF-8");
    expect(env).not.toHaveProperty("ELECTRON_RUN_AS_NODE");
    expect(env).not.toHaveProperty("OCTO_FS_DIR");
    expect(env).not.toHaveProperty("PORT");
  });

  it("identifies itself as a 256-colour terminal", () => {
    const env = terminalEnv(base, "/shim", "1.2.3");
    expect(env).toMatchObject({
      TERM: "xterm-256color",
      COLORTERM: "truecolor",
      TERM_PROGRAM: "Octo",
      TERM_PROGRAM_VERSION: "1.2.3",
    });
  });

  it("supplies a UTF-8 locale when the launch gave none", () => {
    expect(terminalEnv({ PATH: "/bin" }, "/shim", "1").LANG).toBe("en_US.UTF-8");
  });
});
