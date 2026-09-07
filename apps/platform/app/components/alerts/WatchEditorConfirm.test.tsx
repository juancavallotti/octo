import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
  usePathname: () => "/platform/metrics/alerts/w_1",
  useSearchParams: () => new URLSearchParams(),
}));

const deleteWatch = vi.fn();
vi.mock("@/app/model/alerts", () => ({
  createWatch: vi.fn(),
  saveWatch: vi.fn(),
  previewWatch: vi.fn(),
  deleteWatch: (...a: unknown[]) => deleteWatch(...a),
}));

// The editor reads the app list, the broker's destinations and a deployment's
// exported metrics. Mocked at the model layer, like everything else here.
vi.mock("@/app/model/traces", () => ({
  listTraceApps: () => Promise.resolve({ items: [], from: "", to: "" }),
}));
vi.mock("@/app/model/queues", () => ({
  listQueueStats: () => Promise.resolve({ destinations: [] }),
}));
vi.mock("@/app/model/stats", () => ({
  listStatsMetrics: () => Promise.resolve({ items: [] }),
}));

import { ConfirmProvider } from "@/app/components/ConfirmDialog";
import { WatchEditor } from "./WatchEditor";
import { newWatch } from "./catalogue";

/**
 * The editor against the REAL confirm hook, rather than a mocked one.
 *
 * WatchEditor.test.tsx mocks useConfirm, which is right for testing what the
 * editor does with the answer — and it is exactly why it did not notice that the
 * hook throws unless a ConfirmProvider is above it. A mock of a hook is a mock of
 * its contract too, and this one's contract includes needing a provider.
 */
describe("WatchEditor, with the real confirm dialog", () => {
  beforeEach(() => {
    deleteWatch.mockReset().mockResolvedValue(undefined);
  });

  it("renders and deletes inside a provider", async () => {
    const user = userEvent.setup();
    render(
      <ConfirmProvider>
        <WatchEditor initial={newWatch()} watchId="w_1" />
      </ConfirmProvider>,
    );

    await user.click(screen.getByRole("button", { name: "Delete" }));

    // The real dialog appears, and says what goes with the watch.
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent(/evaluation history/i);

    await user.click(within(dialog).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(deleteWatch).toHaveBeenCalledWith("w_1"));
  });
});
