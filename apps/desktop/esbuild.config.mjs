import { spawn } from "node:child_process";
import { cpSync } from "node:fs";
import { createRequire } from "node:module";
import esbuild from "esbuild";

/**
 * Bundle the main and preload processes into dist/.
 *
 * Bundling rather than compiling to loose files is what lets apps/desktop declare
 * no runtime dependencies at all, which in turn is what keeps electron-builder from
 * having to resolve production deps across pnpm's symlink farm — the single most
 * common pnpm + electron-builder failure. Everything this app needs at runtime is
 * either in these bundles, in Electron itself, or in extraResources — electron-updater
 * included, which is a devDependency for exactly this reason.
 *
 *   node esbuild.config.mjs          build once
 *   node esbuild.config.mjs --run    watch, and (re)start Electron on each build
 */

const watch = process.argv.includes("--run");

/**
 * The splash and Settings pages are loaded from disk at runtime rather than inlined,
 * so they have to land beside the bundle. Copied on every build: it is a handful of
 * files, and a stale page is a confusing thing to debug.
 */
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
  // Both preloads run in a sandboxed renderer: no source map comment, since the
  // file is loaded through Electron's own loader and a dangling comment only
  // produces a console warning.
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
 * Restarts are suppressed until the first set of bundles is on disk. Every build
 * fires onEnd during the initial pass, and the debounce below is shorter than the
 * gap between them — so without this, Electron could start against a half-written
 * dist/ and exit, taking the watch with it.
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
    // Wait for it to actually go. The app releases port 8477 while shutting down,
    // and a replacement that raced it would either walk to 8478 — moving the MCP
    // URL mid-session — or, with OCTO_DESKTOP_PORT pinned, fail to bind at all.
    await ended;
  }
  const electron = createRequire(import.meta.url)("electron");
  child = spawn(electron, ["."], { stdio: "inherit", env: process.env });
  // Quitting the app from its own menu should end the watch too, rather than
  // leaving a rebuild loop running against nothing.
  child.on("exit", (code) => process.exit(code ?? 0));
}

/**
 * Coalesce the builds' completions into one restart. Without this, the bundles
 * finishing a few ms apart would start Electron once each — and the first of those
 * would race a preload file that is mid-write.
 */
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
// Build them all, THEN start Electron once, and only then let rebuilds restart it.
// The onEnd hooks fire during this pass while `armed` is still false, so nothing
// launches against a dist/ that is missing the slowest of the bundles.
await Promise.all(contexts.map((c) => c.rebuild()));
copyStatic();
await restart();
armed = true;
await Promise.all(contexts.map((c) => c.watch()));
