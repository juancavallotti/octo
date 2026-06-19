import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Emit a self-contained server bundle (.next/standalone) so the container
  // image (editor/Dockerfile) stays small and doesn't need node_modules at runtime.
  output: "standalone",
};

export default nextConfig;
