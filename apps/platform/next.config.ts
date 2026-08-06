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
  // The NATS client is a Node-only package (net/tls transport); keep it external
  // so it's required at runtime rather than bundled into the server build.
  serverExternalPackages: ["@nats-io/transport-node"],
};

export default nextConfig;
