const { chmodSync, cpSync, existsSync, mkdirSync, readdirSync, rmSync } = require("node:fs");
const path = require("node:path");

/**
 * Copy the staged Next server into the packaged app.
 *
 * A hook rather than an `extraResources` entry because electron-builder excludes
 * `node_modules` from extraResources and no filter re-includes it. afterPack runs
 * before code signing, so everything copied here is signed with the bundle.
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

  preparePty(target, context.arch);
};

// electron-builder's Arch enum, by value.
const ARCH = { 0: "ia32", 1: "x64", 2: "armv7l", 3: "arm64", 4: "universal" };

/**
 * Keep only this build's node-pty prebuild, and make its spawn-helper executable:
 * node-pty 1.1.0 publishes it without the execute bit, and every spawn fails until
 * it has one. Done here because the bundle is signed after this hook and read-only
 * once installed.
 */
function preparePty(resources, arch) {
  const prebuilds = path.join(resources, "app.asar.unpacked", "node_modules", "node-pty", "prebuilds");
  if (!existsSync(prebuilds)) throw new Error(`node-pty is not unpacked at ${prebuilds}`);
  const keep = `${process.platform}-${ARCH[arch]}`;
  for (const dir of readdirSync(prebuilds)) {
    if (dir !== keep) rmSync(path.join(prebuilds, dir), { recursive: true, force: true });
  }
  const helper = path.join(prebuilds, keep, "spawn-helper");
  if (!existsSync(helper)) throw new Error(`node-pty has no prebuild for ${keep}`);
  chmodSync(helper, 0o755);
}
