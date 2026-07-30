import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EditorStateProvider } from "../state/editorState";
import {
  ResourceStoreProvider,
  type ResourceStore,
} from "../providers/ResourceStoreProvider";
import {
  TestSuiteProvider,
  type TestSuiteStore,
} from "../providers/TestSuiteProvider";
import ViewModeToggle from "./ViewModeToggle";

/** Stores that are never called: the toggle only asks whether one is mounted. */
const store = {} as ResourceStore;
const tests = { list: async () => [], canEdit: () => true } as unknown as TestSuiteStore;

function renderToggle(
  resources: ResourceStore | null = null,
  suites: TestSuiteStore | null = null,
) {
  const toggle = (
    <ResourceStoreProvider value={resources}>
      <ViewModeToggle />
    </ResourceStoreProvider>
  );
  return render(
    <EditorStateProvider>
      {suites ? <TestSuiteProvider store={suites}>{toggle}</TestSuiteProvider> : toggle}
    </EditorStateProvider>,
  );
}

describe("ViewModeToggle", () => {
  it("starts on Canvas and switches to YAML on click", async () => {
    renderToggle();
    const canvas = screen.getByRole("button", { name: "Canvas" });
    const yaml = screen.getByRole("button", { name: "YAML" });

    expect(canvas).toHaveAttribute("aria-pressed", "true");
    expect(yaml).toHaveAttribute("aria-pressed", "false");

    await userEvent.click(yaml);

    expect(yaml).toHaveAttribute("aria-pressed", "true");
    expect(canvas).toHaveAttribute("aria-pressed", "false");
  });

  // Offering Resources without a store would land EditorBody on a ResourcesView that
  // has nothing to read — a blank tab with no way to tell why.
  it("hides Resources when no resource store is mounted", () => {
    renderToggle(null);
    expect(screen.queryByRole("button", { name: "Resources" })).toBeNull();
  });

  it("offers Resources when the host backs it", () => {
    renderToggle(store);
    expect(screen.getByRole("button", { name: "Resources" })).toBeInTheDocument();
  });

  // Same rule as Resources, and for the same reason: a tab whose store is absent has
  // nothing to show and no way to explain itself.
  it("hides Testing when no test-suite store is mounted", () => {
    renderToggle(store, null);
    expect(screen.queryByRole("button", { name: "Testing" })).toBeNull();
  });

  it("offers Testing when the host backs it", async () => {
    renderToggle(null, tests);
    const testing = screen.getByRole("button", { name: "Testing" });

    await userEvent.click(testing);
    expect(testing).toHaveAttribute("aria-pressed", "true");
  });

  // The two capabilities are independent: a host may back one and not the other.
  it("gates the two capability tabs separately", () => {
    renderToggle(null, tests);
    expect(screen.queryByRole("button", { name: "Resources" })).toBeNull();
    expect(screen.getByRole("button", { name: "Testing" })).toBeInTheDocument();
  });
});
