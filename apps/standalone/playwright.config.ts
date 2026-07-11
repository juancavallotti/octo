import { defineConfig, devices } from "@playwright/test";
import path from "node:path";

/**
 * Playwright config for the docs screenshot harness (pnpm run screenshots).
 *
 * It boots the standalone editor dev server and drives the /preview route, which
 * renders a repo sample on a pure editor canvas — no auth, no orchestrator, so
 * there is no sign-in wall to clear.
 *
 * The dev server is launched directly (not via the root Taskfile), so we export
 * OCTO_BIN_PATH here to point at the built `octo` binary. The /preview route
 * probes it for the runtime capability schema; without it the palette falls back
 * to the empty bundled JSON and the screenshots come out with an empty palette.
 *
 * Shots render at 1440x900 with deviceScaleFactor 2 so the PNGs are crisp on the
 * landing page. Outputs land in apps/docs/public/screenshots/.
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
