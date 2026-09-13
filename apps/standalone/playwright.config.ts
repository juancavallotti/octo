import { defineConfig, devices } from "@playwright/test";
import path from "node:path";

/**
 * Playwright config for the screenshot harness (pnpm run screenshots): boots this
 * app's dev server and drives the /preview route.
 *
 * OCTO_BIN_PATH is exported here because the server is launched directly rather than
 * through the Taskfile, and /preview probes that binary for the capability schema —
 * without it the palette renders empty.
 *
 * Shots render at 1440x900 with deviceScaleFactor 2 so the PNGs stay crisp.
 */
const OCTO_BIN_PATH =
  process.env.OCTO_BIN_PATH ??
  path.resolve(__dirname, "..", "..", "bin", "octo");
export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  workers: 1,
  reporter: "list",
  use: {
    baseURL: "http://localhost:3000",
    colorScheme: "dark",
  },
  projects: [
    {
      name: "chromium",
      // Spread the device first, then override so the hi-res settings win
      // (Desktop Chrome pins viewport 1280x720 @ 1x otherwise).
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 1440, height: 900 },
        deviceScaleFactor: 2,
      },
    },
  ],
  webServer: {
    command: "pnpm run dev",
    url: "http://localhost:3000/preview?sample=hello-world",
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
    env: { OCTO_BIN_PATH },
  },
});
