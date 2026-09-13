import { defineConfig } from "vitest/config";

/** Node environment: everything under test here is main-process logic. */
export default defineConfig({
  test: {
    environment: "node",
    globals: true,
    include: ["src/**/*.test.ts"],
  },
});
