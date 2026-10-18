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
  it("starts on Flows and switches to Testing on click", async () => {
    renderToggle(null, tests);
    const flows = screen.getByRole("button", { name: "Flows" });
    const testing = screen.getByRole("button", { name: "Testing" });

    expect(flows).toHaveAttribute("aria-pressed", "true");
    await userEvent.click(testing);
    expect(testing).toHaveAttribute("aria-pressed", "true");
    expect(flows).toHaveAttribute("aria-pressed", "false");
  });

  // Canvas and YAML are views of the open file, so they sit beside its name instead.
  it("offers no Canvas or YAML tab", () => {
    renderToggle(store, tests);
    expect(screen.queryByRole("button", { name: "Canvas" })).toBeNull();
    expect(screen.queryByRole("button", { name: "YAML" })).toBeNull();
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
