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
 * either in these two files, in Electron itself, or in extraResources.
 *
 *   node esbuild.config.mjs          build once
 *   node esbuild.config.mjs --run    watch, and (re)start Electron on each build
 */

const watch = process.argv.includes("--run");

/**
 * The splash page is loaded from disk at runtime rather than inlined, so it has to
 * land beside the bundle. Copied on every build: it is two files, and a stale
 * splash is a confusing thing to debug.
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
  {
    ...common,
    entryPoints: ["src/preload/index.ts"],
    outfile: "dist/preload/index.js",
    // The preload runs in a sandboxed renderer: no source map comment, since the
    // file is loaded through Electron's own loader and a dangling comment only
    // produces a console warning.
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

function restart() {
  if (child) {
    // Drop the exit listener first: this kill is a restart, not the user quitting.
    child.removeAllListeners("exit");
    child.kill();
  }
  const electron = createRequire(import.meta.url)("electron");
  child = spawn(electron, ["."], { stdio: "inherit", env: process.env });
  // Quitting the app from its own menu should end the watch too, rather than
  // leaving a rebuild loop running against nothing.
  child.on("exit", (code) => process.exit(code ?? 0));
}

/**
 * Coalesce the two builds' completions into one restart. Without this, main and
 * preload finishing a few ms apart would start Electron twice — and the first of
 * those would race a preload file that is mid-write.
 */
function scheduleRestart() {
  clearTimeout(restartTimer);
  restartTimer = setTimeout(restart, 50);
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
// Build both before watching, so the first Electron start never sees a half-written
// dist/ — the onEnd hooks fire during these, and the debounce collapses them.
await Promise.all(contexts.map((c) => c.rebuild()));
await Promise.all(contexts.map((c) => c.watch()));
