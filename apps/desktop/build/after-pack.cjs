const { cpSync, existsSync, mkdirSync } = require("node:fs");
const path = require("node:path");

/**
 * Copy the staged Next server into the packaged app.
 *
 * This is a hook rather than an `extraResources` entry for one blunt reason:
 * electron-builder excludes `node_modules` from extraResources and no filter
 * re-includes it. The copy silently succeeds, the app packages and signs cleanly,
 * and then cannot start its own server — which is exactly the kind of failure
 * worth spending a hook to avoid.
 *
 * afterPack runs before code signing, so everything copied here is signed with
 * the rest of the bundle.
 */
exports.default = async function afterPack(context) {
  const resources = path.join(context.appOutDir, `${context.packager.appInfo.productFilename}.app`, "Contents", "Resources");
  const target = process.platform === "darwin" && existsSync(resources)
    ? resources
    : path.join(context.appOutDir, "resources");

  const from = path.join(context.packager.projectDir, "build", "server");
  if (!existsSync(from)) {
    throw new Error(`no staged server at ${from} — run \`task desktop:stage\` first`);
  }

  const to = path.join(target, "server");
  mkdirSync(to, { recursive: true });
  // verbatimSymlinks keeps pnpm's relative links as links; dereferencing them
  // would multiply the tree by every duplicated package.
  cpSync(from, to, { recursive: true, verbatimSymlinks: true });

  const entry = path.join(to, "apps", "standalone", "server.js");
  if (!existsSync(entry)) throw new Error(`staged server has no entry point at ${entry}`);
  console.log(`  • staged editor server copied into the bundle  path=${to}`);
};
