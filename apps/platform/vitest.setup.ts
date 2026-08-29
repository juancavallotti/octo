import "@testing-library/jest-dom/vitest";
import { afterEach } from "vitest";
import { cleanup } from "@testing-library/react";

// jsdom implements no layout, so it ships no ResizeObserver. A component that
// measures itself has to keep working without one — it falls back to an assumed
// size — but it must still be able to *construct* one, so this is a stub that
// observes nothing rather than a fake that reports sizes jsdom does not have.
if (!("ResizeObserver" in globalThis)) {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
}

afterEach(() => {
  cleanup();
});
