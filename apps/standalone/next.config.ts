import type { NextConfig } from "next";
import path from "path";

const nextConfig: NextConfig = {
  // Self-contained server bundle for the public Docker image.
  output: "standalone",
  // pnpm workspace: trace from the repo root so the standalone bundle picks up the
  // hoisted node_modules and the server lands at apps/standalone/.next/standalone/.
  outputFileTracingRoot: path.join(__dirname, "../../"),
  // Workspace packages ship as untranspiled TS source; let Next compile them.
  transpilePackages: ["@octo/editor", "@octo/http", "@octo/run-host"],
};

export default nextConfig;
