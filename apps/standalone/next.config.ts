import type { NextConfig } from "next";
import path from "path";

const nextConfig: NextConfig = {
  // Self-contained server bundle for the public Docker image.
  output: "standalone",
  // pnpm workspace: trace from the repo root so the standalone bundle picks up the
  // hoisted node_modules and the server lands at apps/standalone/.next/standalone/.
  outputFileTracingRoot: path.join(__dirname, "../../"),
  // Workspace packages ship as untranspiled TS source; let Next compile them.
  transpilePackages: [
    "@octo/editor",
    "@octo/events",
    "@octo/http",
    "@octo/run-host",
  ],
  // No runtime image optimization — and therefore no `sharp`.
  //
  // The app has exactly one <Image>: the 24px logo in StandaloneHeader. Serving
  // it through the optimizer costs a 20MB arch-specific native module
  // (@img/sharp-<os>-<arch>) in the standalone output, which is dead weight in
  // the Docker image and actively wrong for the desktop app: the staged server
  // would carry whatever architecture ran `next build`, so an x64 .app built on
  // an arm64 Mac would ship an arm64 binary. The logo asset is small enough
  // (see public/octo-logo.png) that optimizing it saves nothing.
  //
  // Note this setting does not by itself keep sharp out of .next/standalone —
  // Next traces it from next-server.js regardless of config, and
  // outputFileTracingExcludes only applies to page traces. What it does is make
  // the module unreachable at runtime, which is what lets the desktop app prune
  // it while staging (see apps/desktop/Taskfile.yml).
  images: { unoptimized: true },
  // Don't 308-redirect trailing slashes away. The run reverse proxy
  // (app/editor/runs/[ns]/[[...path]]) advertises its test URL *with* a trailing
  // slash (`/editor/runs/<ns>/`) so relative links in a served integration
  // resolve under the run prefix. With the default redirect, Next strips that
  // slash at the routing layer before the proxy handler runs, so the advertised
  // URL never reaches the integration. Let the proxy own trailing slashes.
  skipTrailingSlashRedirect: true,
};

export default nextConfig;
