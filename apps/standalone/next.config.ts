import type { NextConfig } from "next";
import path from "path";

const nextConfig: NextConfig = {
  // Self-contained server bundle.
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
  // No runtime image optimization — and therefore no `sharp`. The app has one
  // <Image>, a 24px logo, and optimizing it would cost a 20MB architecture-specific
  // native module in the standalone output.
  //
  // This does not by itself keep sharp out of .next/standalone — Next traces it from
  // next-server.js regardless, and outputFileTracingExcludes only applies to page
  // traces — but it does make the module unreachable at runtime, so it can be pruned.
  images: { unoptimized: true },
  // Don't 308-redirect trailing slashes away: the run reverse proxy advertises its
  // test URL with one (`/editor/runs/<ns>/`) so relative links resolve under the run
  // prefix, and the default redirect strips it before the handler runs.
  skipTrailingSlashRedirect: true,
};

export default nextConfig;
