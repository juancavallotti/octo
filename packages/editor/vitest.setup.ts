import "@testing-library/jest-dom/vitest";
import { afterEach, beforeEach } from "vitest";
import { cleanup } from "@testing-library/react";
import { setCapabilities, type Capabilities } from "./src/app/schema";
import fixture from "./src/test/capabilities.fixture.json";

// The editor renders whatever capability schema it is handed; the shipped bundle
// is only an empty fallback (the runtime generates the real catalogue via
// `octo schema`). Inject a representative fixture before each test so the
// component and model tests exercise real block/connector/source specs — the
// same coverage the bundled schema used to provide. A file testing the
// empty-fallback path overrides this locally with setCapabilities(null).
beforeEach(() => setCapabilities(fixture as Capabilities));

afterEach(() => {
  cleanup();
  setCapabilities(null);
});
