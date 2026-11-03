import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

const listStatsMetrics = vi.fn();
vi.mock("@/app/model/stats", () => ({
  listStatsMetrics: (...a: unknown[]) => listStatsMetrics(...a),
}));

import { MetricPicker } from "./MetricPicker";

// A histogram reaches the stats store only as its _bucket, _sum and _count
// series. The picker has to offer a name from that set, or a deployment that has
// served traffic still shows the duration metric as never reported.
describe("MetricPicker", () => {
  it("marks the stored histogram series as reported", async () => {
    listStatsMetrics.mockResolvedValue({
      items: [
        { name: "octo_flow_duration_seconds_bucket" },
        { name: "octo_flow_duration_seconds_sum" },
        { name: "octo_flow_duration_seconds_count" },
      ],
    });
    render(<MetricPicker deploymentId="dep-1" value="" onChange={() => {}} />);

    await userEvent.click(await screen.findByText("Search metrics…"));
    // The name and its "not reported yet" marker share one parent in the row.
    const name = await screen.findByText("octo_flow_duration_seconds_sum");

    expect(name.parentElement?.textContent).toBe("octo_flow_duration_seconds_sum");
    expect(screen.queryByText("octo_flow_duration_seconds")).toBeNull();
  });
});
