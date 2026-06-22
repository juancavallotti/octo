import { defineConfig, devices } from "@playwright/test";

/**
 * Playwright config for the docs screenshot harness (npm run screenshots).
 *
 * It boots the editor dev server with the OIDC env vars CLEARED so `authEnabled`
 * is false and the proxy/middleware is a no-op — Playwright can drive the editor
 * with no sign-in wall. @next/env won't override a process var that's already
 * set, so passing them empty here wins over editor/.env.
 *
 * Shots render at 1600x900 with deviceScaleFactor 2 (effective 3200x1800) so the
 * PNGs are crisp on the landing page. Outputs land in docs/assets/screenshots/.
 */
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
    command: "npm run dev",
    url: "http://localhost:3000/preview?sample=hello-world",
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
    env: {
      ...process.env,
      // Disable SSO so the proxy is a no-op (see auth.config.ts authEnabled).
      AUTH_EETR_ISSUER: "",
      AUTH_SECRET: "",
    },
  },
});
