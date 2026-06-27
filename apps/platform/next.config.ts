import type { NextConfig } from "next";
import path from "path";

const nextConfig: NextConfig = {
  // Emit a self-contained server bundle (.next/standalone) so the container
  // image (apps/platform/Dockerfile) stays small and doesn't need node_modules
  // at runtime.
  output: "standalone",
  // This app lives in a pnpm workspace; trace files from the repo root so the
  // standalone bundle picks up the hoisted (symlinked) node_modules and the
  // server is emitted at .next/standalone/apps/platform/server.js.
  outputFileTracingRoot: path.join(__dirname, "../../"),
  // Workspace packages ship as untranspiled TS source; let Next compile them.
  transpilePackages: [
    "@octo/editor",
    "@octo/events",
    "@octo/http",
    "@octo/mcp",
    "@octo/run-host",
  ],
};

export default nextConfig;
