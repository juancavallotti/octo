import { spawn } from "node:child_process";
import { cpSync } from "node:fs";
import { createRequire } from "node:module";
import esbuild from "esbuild";

/**
 * Bundle the main and preload processes into dist/.
 *
 * Bundling rather than compiling to loose files is what lets this app declare no
 * runtime dependencies at all: everything it needs is in these bundles, in Electron
 * itself, or in extraResources — electron-updater included, which is why that is a
 * devDependency.
 *
 *   node esbuild.config.mjs          build once
 *   node esbuild.config.mjs --run    watch, and (re)start Electron on each build
 */

const watch = process.argv.includes("--run");

/** The splash and Settings pages are loaded from disk, so they land beside the bundle. */
function copyStatic() {
  cpSync("src/main/static", "dist/main/static", { recursive: true });
}

const common = {
  bundle: true,
  platform: "node",
  // Electron 44 ships Node 24; there is no downlevelling to do.
  target: "node22",
  format: "cjs",
  sourcemap: true,
  // Provided by the runtime, never bundled.
  external: ["electron"],
  logLevel: "info",
};

const builds = [
  { ...common, entryPoints: ["src/main/index.ts"], outfile: "dist/main/index.js" },
  // Both preloads run in a sandboxed renderer, which loads them through Electron's
  // own loader: an external source map comment only warns.
  {
    ...common,
    entryPoints: ["src/preload/index.ts"],
    outfile: "dist/preload/index.js",
    sourcemap: "inline",
  },
  {
    ...common,
    entryPoints: ["src/preload/settings.ts"],
    outfile: "dist/preload/settings.js",
    sourcemap: "inline",
  },
];

if (!watch) {
  await Promise.all(builds.map((b) => esbuild.build(b)));
  copyStatic();
  process.exit(0);
}

/** The running Electron, restarted on each successful rebuild. */
let child = null;
let restartTimer = null;
/**
 * Restarts are suppressed until the first set of bundles is on disk: every build
 * fires onEnd during the initial pass, and the debounce below is shorter than the
 * gap between them.
 */
let armed = false;

async function restart() {
  if (child) {
    // Drop the exit listener first: this kill is a restart, not the user quitting.
    const dying = child;
    dying.removeAllListeners("exit");
    const ended = new Promise((resolve) => dying.once("exit", resolve));
    dying.kill();
    child = null;
    // Wait for it to actually go: the port is only released during shutdown, and a
    // replacement that raced it would move or fail to bind.
    await ended;
  }
  const electron = createRequire(import.meta.url)("electron");
  child = spawn(electron, ["."], { stdio: "inherit", env: process.env });
  // Quitting the app from its own menu ends the watch too.
  child.on("exit", (code) => process.exit(code ?? 0));
}

/** Coalesce the builds' completions into one restart. */
function scheduleRestart() {
  if (!armed) return;
  clearTimeout(restartTimer);
  restartTimer = setTimeout(() => void restart(), 50);
}

const notify = {
  name: "restart-electron",
  setup(build) {
    build.onEnd((result) => {
      if (result.errors.length === 0) {
        copyStatic();
        scheduleRestart();
      }
    });
  },
};

const contexts = await Promise.all(
  builds.map((b) => esbuild.context({ ...b, plugins: [notify] })),
);
// Build them all, THEN start Electron once, and only then let rebuilds restart it:
// nothing must launch against a dist/ missing the slowest of the bundles.
await Promise.all(contexts.map((c) => c.rebuild()));
copyStatic();
await restart();
armed = true;
await Promise.all(contexts.map((c) => c.watch()));
