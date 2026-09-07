import { loadEnvConfig } from "@next/env";
import type { NextConfig } from "next";
import path from "path";

// Read the repo root's .env, not this app's.
//
// Next loads .env from the project directory only — there is no monorepo
// awareness and no envDir option; the documented way to read one from anywhere
// else is @next/env's loadEnvConfig, which is what this is.
//
// It is the root file because a second .env inside apps/platform was a hazard
// rather than a convenience. This directory is the platform image's build
// context AND its DevSpace sync path, so an SSO-configured developer baked their
// client secret into every local image and pushed it into the running pod. The
// root file is in neither.
//
// Only dev and build read this. A deployed container has no .env at all: its
// environment comes from the chart, which is the one place secrets belong.
loadEnvConfig(path.join(__dirname, "../.."));

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
