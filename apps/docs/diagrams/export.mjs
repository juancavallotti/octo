/**
 * Render an .excalidraw scene to a PNG for the docs.
 *
 * The scene is drawn by Excalidraw itself in a headless Chrome, then the canvas
 * is cropped to the drawing's own bounds, so what ships is what the editor shows
 * rather than a re-implementation of its renderer.
 *
 *   node export.mjs self-healing-loop
 *
 * It serves this directory itself: the page has to fetch the scene, and a
 * file:// page cannot.
 */
import { spawn } from "node:child_process";
import { chromium } from "/Users/juancavallotti/Documents/development/octo/node_modules/.pnpm/playwright@1.62.1/node_modules/playwright/index.mjs";

const server = spawn("python3", ["-m", "http.server", "8123"], { stdio: "ignore" });
const stop = () => server.kill();
process.on("exit", stop);
await new Promise((r) => setTimeout(r, 700));

const name = process.argv[2];
const out = `/Users/juancavallotti/Documents/development/octo/apps/docs/public/diagrams/${name}.png`;

const browser = await chromium.launch({ channel: "chrome" });
const page = await browser.newPage({ viewport: { width: 1600, height: 1100 } });
page.on("pageerror", (e) => console.log("pageerror:", e.message));
await page.goto(`http://localhost:8123/viewer.html?f=${name}.excalidraw`, { waitUntil: "networkidle" });
await page.waitForFunction(() => document.title !== "diagram export", null, { timeout: 20000 });
const title = await page.title();
if (title !== "ready") {
  console.error(title);
  await browser.close();
  stop();
  process.exit(1);
}
await page.locator("#shot").screenshot({ path: out });
console.log("wrote", out);
await browser.close();
stop();
