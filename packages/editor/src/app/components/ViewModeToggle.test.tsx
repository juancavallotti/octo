import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EditorStateProvider } from "../state/editorState";
import {
  ResourceStoreProvider,
  type ResourceStore,
} from "../providers/ResourceStoreProvider";
import ViewModeToggle from "./ViewModeToggle";

/** A store that is never called: the toggle only asks whether one is mounted. */
const store = {} as ResourceStore;

function renderToggle(resources: ResourceStore | null = null) {
  return render(
    <EditorStateProvider>
      <ResourceStoreProvider value={resources}>
        <ViewModeToggle />
      </ResourceStoreProvider>
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
});
