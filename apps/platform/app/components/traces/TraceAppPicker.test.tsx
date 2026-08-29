import { describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import TraceAppPicker from "./TraceAppPicker";
import type { TraceApp } from "@/app/model/traces";

/**
 * The app picker is where two of the trace store's careful distinctions have to
 * survive contact with a screen: that an unpriced call is not a free one, and
 * that dropped records belong to an app rather than to any particular trace.
 * Both are easy to render away, and neither looks wrong once you have.
 */

const APP: TraceApp = {
  deploymentId: "dep-1",
  integrationId: "int-1",
  appName: "Orders",
  appVersion: "v1.0",
  traces: 12,
  failed: 0,
  lastSeenAt: new Date().toISOString(),
  costUsd: 0.42,
  unpricedCalls: 0,
  droppedRecords: 0,
};

function renderPicker(
  apps: TraceApp[],
  over: Partial<Parameters<typeof TraceAppPicker>[0]> = {},
) {
  const onSelect = vi.fn();
  render(
    <TraceAppPicker
      apps={apps}
      window="24h"
      onWindowChange={vi.fn()}
      loading={false}
      selectedId={null}
      selectedVersion={null}
      onSelect={onSelect}
      onRefresh={vi.fn()}
      {...over}
    />,
  );
  return onSelect;
}

/** Everything below the trigger only exists once the list is open. */
async function open() {
  await userEvent.click(screen.getByRole("button", { name: "App" }));
}

/**
 * The app rows, scoped to the picker's own list — the window beside it is a
 * native select, and its options answer to the same role.
 */
function appRows() {
  return within(screen.getByRole("listbox")).getAllByRole("option");
}

describe("TraceAppPicker", () => {
  it("offers the window before the app, not after it", async () => {
    // The app list is counted over the window, so an app is only in it — and
    // only says "12 traces" — because of a window already chosen. Offered
    // afterwards it asks someone to pick from a list narrowed by something they
    // have not been shown yet.
    renderPicker([APP]);
    const toolbar = screen.getByLabelText("Window").parentElement!;
    const controls = [...toolbar.children];
    expect(controls.indexOf(screen.getByLabelText("Window"))).toBeLessThan(
      controls.findIndex((c) => c.contains(screen.getByRole("button", { name: "App" }))),
    );

    await open();
    expect(screen.getByText("12 traces")).toBeInTheDocument();
  });

  it("changes the window without an app being chosen", async () => {
    // It measures the app list too, so locking it until an app is picked would
    // put the only way of widening the search behind the thing being looked for.
    const onWindowChange = vi.fn();
    renderPicker([], { onWindowChange });
    await userEvent.selectOptions(screen.getByLabelText("Window"), "7d");
    expect(onWindowChange).toHaveBeenCalledWith("7d");
  });

  it("shows a fully priced app as its cost", async () => {
    renderPicker([APP]);
    await open();
    expect(screen.getByText("$0.4200")).toBeInTheDocument();
  });

  it("marks a partly priced app as a lower bound", async () => {
    renderPicker([{ ...APP, unpricedCalls: 3 }]);
    await open();
    expect(screen.getByText("≥ $0.4200")).toBeInTheDocument();
  });

  it("never shows an app whose calls could not be priced as free", async () => {
    renderPicker([{ ...APP, costUsd: 0, unpricedCalls: 2 }]);
    await open();
    expect(screen.getByText("unpriced")).toBeInTheDocument();
    expect(screen.queryByText("$0")).not.toBeInTheDocument();
  });

  it("reports dropped records against the app, with why they stop there", async () => {
    renderPicker([{ ...APP, droppedRecords: 40 }]);
    await open();
    const dropped = screen.getByText("40 records dropped");
    expect(dropped).toBeInTheDocument();
    expect(dropped.getAttribute("title")).toContain("no trace id");
  });

  it("shows a rollout as the two apps it is", async () => {
    // One deployment, two versions. Collapsing them would blend the cost of the
    // version being investigated into the one it replaced.
    renderPicker([APP, { ...APP, appVersion: "v2.0", traces: 3 }]);
    await open();
    expect(screen.getByText("v1.0")).toBeInTheDocument();
    expect(screen.getByText("v2.0")).toBeInTheDocument();
  });

  it("selects a version, not just a deployment", async () => {
    const onSelect = renderPicker([APP, { ...APP, appVersion: "v2.0" }]);
    await open();
    await userEvent.click(screen.getByText("v2.0"));
    expect(onSelect).toHaveBeenCalledWith(
      expect.objectContaining({ appVersion: "v2.0" }),
    );
  });

  it("marks only the selected version as current", async () => {
    renderPicker([APP, { ...APP, appVersion: "v2.0" }], {
      selectedId: "dep-1",
      selectedVersion: "v2.0",
    });
    await open();
    const current = appRows().filter(
      (o) => o.getAttribute("aria-selected") === "true",
    );
    expect(current).toHaveLength(1);
    expect(current[0]).toHaveTextContent("v2.0");
  });

  it("marks an untagged app as selected too", async () => {
    // A deployment from before version tags reports "", and the path omits the
    // version segment entirely — so the two spellings of "no version" have to
    // compare equal, or that app can never look like the one you picked.
    renderPicker([{ ...APP, appVersion: "" }], {
      selectedId: "dep-1",
      selectedVersion: null,
    });
    expect(screen.getByRole("button", { name: "App" })).toHaveTextContent("Orders");
    await open();
    expect(appRows()[0]).toHaveAttribute("aria-selected", "true");
  });

  it("finds an app by a name the reader half-remembers", async () => {
    // The whole reason this stopped being a scrollable column: a long deployment
    // list is one someone knows the name in and not the position of.
    renderPicker([
      { ...APP, deploymentId: "dep-2", appName: "Billing" },
      { ...APP, appName: "Orders" },
    ]);
    await open();
    await userEvent.type(screen.getByLabelText("Search app"), "bling");
    expect(appRows()[0]).toHaveTextContent("Billing");
  });

  it("explains an empty list rather than just being empty", async () => {
    // Nothing here is the expected state: tracing is off unless someone turned
    // it on, so "no traces" is much more likely to be a setting than a problem.
    renderPicker([]);
    await open();
    expect(screen.getByText(/tracing is off by default/i)).toBeInTheDocument();
  });
});
