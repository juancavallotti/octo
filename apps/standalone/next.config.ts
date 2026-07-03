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
  // Don't 308-redirect trailing slashes away. The run reverse proxy
  // (app/editor/runs/[ns]/[[...path]]) advertises its test URL *with* a trailing
  // slash (`/editor/runs/<ns>/`) so relative links in a served integration
  // resolve under the run prefix. With the default redirect, Next strips that
  // slash at the routing layer before the proxy handler runs, so the advertised
  // URL never reaches the integration. Let the proxy own trailing slashes.
  skipTrailingSlashRedirect: true,
};

export default nextConfig;
