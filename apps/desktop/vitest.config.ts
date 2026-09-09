import { defineConfig } from "vitest/config";

/**
 * Node environment: everything under test here is main-process logic. The parts
 * that need Electron itself (windows, dialogs, menus) are deliberately not tested
 * in unit tests — they are thin wrappers, and mocking Electron to assert that a
 * wrapper called a wrapper proves nothing. What IS tested is the logic that would
 * be wrong silently: port selection, state persistence, path resolution.
 */
export default defineConfig({
  test: {
    environment: "node",
    globals: true,
    include: ["src/**/*.test.ts"],
  },
});
