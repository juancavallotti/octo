import { test, expect } from "@playwright/test";

/**
 * Renders every gallery sample in the real editor and captures a full-editor
 * viewport screenshot — showing the component palette, the flow on the canvas,
 * and the settings panel together. Each PNG is shown inside that sample's panel
 * on the docs site (assets/screenshots/sample-<id>.png) and a couple are reused
 * in the What's New section. Run with `npm run screenshots` (boots the editor
 * with SSO disabled via playwright.config.ts). See docs/SCREENSHOTS.md.
 */

// docs gallery id ("id" used by app.js / the image name) -> samples/<file>.yaml
const SAMPLES = [
  { id: "hello-world", file: "hello-world" },
  { id: "http-orders", file: "http-orders" },
  { id: "db-orders", file: "db-orders" },
  { id: "weather", file: "weather" },
  { id: "flow-to-flow", file: "flow-to-flow-http" },
  { id: "builtins", file: "builtins-demo" },
  { id: "ai-router", file: "ai-router" },
  { id: "ai-agent", file: "ai-agent" },
  { id: "ai-mapping", file: "ai-mapping" },
  { id: "ai-retry", file: "ai-retry" },
  { id: "error-handling", file: "error-handling" },
  { id: "file-logger", file: "file-logger" },
  { id: "heartbeat", file: "heartbeat" },
];

const OUT_DIR = "../docs/assets/screenshots";

for (const { id, file } of SAMPLES) {
  test(`screenshot sample ${id}`, async ({ page }) => {
    await page.goto(`/preview?sample=${file}`);

    // The sample loads client-side (fetch + dispatch); wait for a flow card.
    await expect(page.locator("main.canvas-grid section").first()).toBeVisible({
      timeout: 15_000,
    });
    // Let layout settle (fonts, the arrows drawn between blocks) before shooting.
    await page.waitForTimeout(500);

    // Shoot the whole editor viewport: palette + canvas + settings panel. This
    // shows the product, not just the flow, and keeps a consistent 16:10 frame.
    await page.screenshot({ path: `${OUT_DIR}/sample-${id}.png` });
  });
}
