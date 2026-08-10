import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import TraceAppList from "./TraceAppList";
import type { TraceApp } from "@/app/model/traces";

/**
 * The app list is where two of the trace store's careful distinctions have to
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

const WINDOW = {
  from: "2026-08-08T12:00:00Z",
  to: "2026-08-09T12:00:00Z",
};

function renderList(apps: TraceApp[], over: Partial<Parameters<typeof TraceAppList>[0]> = {}) {
  const onSelect = vi.fn();
  render(
    <TraceAppList
      apps={apps}
      window={WINDOW}
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

describe("TraceAppList", () => {
  it("names the window its counts were measured over", () => {
    // "12 traces" says nothing on its own — an hour and a week are very
    // different claims, and the service picks the default, not this client.
    renderList([APP]);
    expect(screen.getByText("last 24 hours")).toBeInTheDocument();
    expect(screen.getByText("12 traces")).toBeInTheDocument();
  });

  it("shows a fully priced app as its cost", () => {
    renderList([APP]);
    expect(screen.getByText("$0.4200")).toBeInTheDocument();
  });

  it("marks a partly priced app as a lower bound", () => {
    renderList([{ ...APP, unpricedCalls: 3 }]);
    expect(screen.getByText("≥ $0.4200")).toBeInTheDocument();
  });

  it("never shows an app whose calls could not be priced as free", () => {
    renderList([{ ...APP, costUsd: 0, unpricedCalls: 2 }]);
    expect(screen.getByText("unpriced")).toBeInTheDocument();
    expect(screen.queryByText("$0")).not.toBeInTheDocument();
  });

  it("reports dropped records against the app, with why they stop there", () => {
    renderList([{ ...APP, droppedRecords: 40 }]);
    const dropped = screen.getByText("40 records dropped");
    expect(dropped).toBeInTheDocument();
    expect(dropped.getAttribute("title")).toContain("no trace id");
  });

  it("shows a rollout as the two apps it is", () => {
    // One deployment, two versions. Collapsing them would blend the cost of the
    // version being investigated into the one it replaced.
    renderList([APP, { ...APP, appVersion: "v2.0", traces: 3 }]);
    expect(screen.getByText("v1.0")).toBeInTheDocument();
    expect(screen.getByText("v2.0")).toBeInTheDocument();
  });

  it("selects a version, not just a deployment", async () => {
    const onSelect = renderList([APP, { ...APP, appVersion: "v2.0" }]);
    await userEvent.click(screen.getByText("v2.0"));
    expect(onSelect).toHaveBeenCalledWith(expect.objectContaining({ appVersion: "v2.0" }));
  });

  it("marks only the selected version as current", () => {
    renderList([APP, { ...APP, appVersion: "v2.0" }], {
      selectedId: "dep-1",
      selectedVersion: "v2.0",
    });
    const current = screen.getAllByRole("button").filter((b) => b.getAttribute("aria-current"));
    expect(current).toHaveLength(1);
    expect(current[0]).toHaveTextContent("v2.0");
  });

  it("explains an empty list rather than just being empty", () => {
    // Nothing here is the expected state: tracing is off unless someone turned
    // it on, so "no traces" is much more likely to be a setting than a problem.
    renderList([]);
    expect(screen.getByText(/tracing is off by default/i)).toBeInTheDocument();
  });
});
