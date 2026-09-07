import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const push = vi.fn();
const refresh = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push, refresh }),
  usePathname: () => "/platform/metrics/alerts/new",
  useSearchParams: () => new URLSearchParams(),
}));

const createWatch = vi.fn();
const saveWatch = vi.fn();
const deleteWatch = vi.fn();
const previewWatch = vi.fn();
vi.mock("@/app/model/alerts", () => ({
  createWatch: (...a: unknown[]) => createWatch(...a),
  saveWatch: (...a: unknown[]) => saveWatch(...a),
  deleteWatch: (...a: unknown[]) => deleteWatch(...a),
  previewWatch: (...a: unknown[]) => previewWatch(...a),
}));

const listTraceApps = vi.fn();
vi.mock("@/app/model/traces", () => ({
  listTraceApps: () => listTraceApps(),
}));

// The app picker lists what is deployed, so the registry is the fixture and the
// trace list only decorates it.
const listAllDeployments = vi.fn();
vi.mock("@/app/model/orchestrator", () => ({
  listAllDeployments: () => listAllDeployments(),
}));

vi.mock("@/app/model/queues", () => ({
  listQueueStats: () => Promise.resolve({ destinations: [] }),
}));
vi.mock("@/app/model/stats", () => ({
  listStatsMetrics: () => Promise.resolve({ items: [] }),
}));

const confirm = vi.fn();
vi.mock("@/app/components/ConfirmDialog", () => ({
  useConfirm: () => confirm,
}));

import { WatchEditor } from "./WatchEditor";
import { newWatch } from "./catalogue";

const DEPLOYMENT = {
  id: "d_1",
  integrationId: "i_1",
  name: "checkout",
  lastUpdated: "2026-09-06T09:00:00Z",
};

const APP = {
  deploymentId: "d_1",
  integrationId: "i_1",
  appName: "checkout",
  appVersion: "v1",
  lastSeenAt: "2026-09-06T10:00:00Z",
  traces: 10,
  failed: 1,
  costUsd: 0,
  unpricedCalls: 0,
  droppedRecords: 0,
};

function renderEditor(watchId: string | null = null) {
  return render(<WatchEditor initial={newWatch()} watchId={watchId} />);
}

/**
 * Choose the app in step 1, which everything below it is measured over.
 *
 * The rows only exist once the popover is open, and they are scoped to its own
 * listbox — the selects on the form answer to the same role.
 */
async function pickApp(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole("button", { name: "Application" }));
  const list = await screen.findByRole("listbox");
  await user.click(within(list).getByRole("option", { name: /checkout/ }));
}

describe("WatchEditor", () => {
  beforeEach(() => {
    push.mockReset();
    createWatch.mockReset().mockResolvedValue({ id: "w_1" });
    saveWatch.mockReset().mockResolvedValue({ id: "w_1" });
    deleteWatch.mockReset().mockResolvedValue(undefined);
    previewWatch.mockReset();
    confirm.mockReset().mockResolvedValue(true);
    listTraceApps
      .mockReset()
      .mockResolvedValue({ items: [APP], from: "", to: "" });
    listAllDeployments.mockReset().mockResolvedValue([DEPLOYMENT]);
  });

  // The picker lists what is deployed, not what has already reported. Sourcing
  // it from telemetry meant an app could only be watched once it had produced
  // some, which defeats the point: the watch worth writing is the one armed
  // before the first failure.
  it("offers a deployed app that has never reported", async () => {
    const user = userEvent.setup();
    listAllDeployments.mockResolvedValue([
      DEPLOYMENT,
      {
        id: "d_2",
        integrationId: "i_2",
        name: "seneca-quote",
        lastUpdated: "2026-09-01T09:00:00Z",
      },
    ]);
    // Only the first has ever traced.
    listTraceApps.mockResolvedValue({ items: [APP], from: "", to: "" });
    renderEditor();

    await user.click(await screen.findByRole("button", { name: "Application" }));
    const list = await screen.findByRole("listbox");
    expect(within(list).getByRole("option", { name: /checkout/ })).toBeVisible();
    const quiet = within(list).getByRole("option", { name: /seneca-quote/ });
    expect(quiet).toBeVisible();
    // And it is marked, because there is nothing to preview a watch against.
    expect(quiet).toHaveTextContent("no data yet");
    // The one that has reported carries no marker at all.
    expect(
      within(list).getByRole("option", { name: /checkout/ }),
    ).not.toHaveTextContent("no data yet");
  });

  // The app is the only answer everything else depends on, so it is asked first
  // and written onto every condition rather than typed once per condition.
  it("scopes every condition to the app chosen at the top", async () => {
    const user = userEvent.setup();
    renderEditor();
    await pickApp(user);

    await user.type(screen.getByLabelText("Name"), "checkout errors");
    await user.click(screen.getByRole("button", { name: /add a condition/i }));
    await user.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => expect(createWatch).toHaveBeenCalled());
    const sent = createWatch.mock.calls[0][0] as {
      conditions: { scope: Record<string, string> }[];
    };
    expect(sent.conditions).toHaveLength(2);
    // Traces are scoped by integration, which survives a rollout where a
    // deployment id does not.
    for (const condition of sent.conditions) {
      expect(condition.scope.integrationId).toBe("i_1");
      expect(condition.scope.deploymentId).toBeUndefined();
    }
  });

  // Logs have no integration column, so the same app is a different predicate.
  it("scopes a log condition by app name instead", async () => {
    const user = userEvent.setup();
    renderEditor();
    await pickApp(user);

    await user.type(screen.getByLabelText("Name"), "noisy logs");
    await user.selectOptions(
      screen.getByLabelText("Condition 1 measure"),
      "logs:events",
    );
    await user.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => expect(createWatch).toHaveBeenCalled());
    const sent = createWatch.mock.calls[0][0] as {
      conditions: { scope: Record<string, string> }[];
    };
    expect(sent.conditions[0].scope.appName).toBe("checkout");
    expect(sent.conditions[0].scope.integrationId).toBeUndefined();
  });

  it("adds and removes conditions", async () => {
    const user = userEvent.setup();
    renderEditor();

    expect(
      screen.queryByRole("button", { name: "Remove condition 1" }),
    ).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /add a condition/i }));
    expect(screen.getByLabelText("Condition 2 measure")).toBeInTheDocument();

    await user.click(
      screen.getByRole("button", { name: "Remove condition 2" }),
    );
    expect(
      screen.queryByLabelText("Condition 2 measure"),
    ).not.toBeInTheDocument();
  });

  // A dropdown with one option is a question with one answer.
  it("only offers an aggregate when there is a choice", async () => {
    const user = userEvent.setup();
    renderEditor();

    // Error rate is only ever a rate.
    expect(
      screen.queryByLabelText("Condition 1 aggregate"),
    ).not.toBeInTheDocument();

    await user.selectOptions(
      screen.getByLabelText("Condition 1 measure"),
      "traces:duration_ns",
    );
    expect(screen.getByLabelText("Condition 1 aggregate")).toBeInTheDocument();
  });

  it("resets a condition's parameters when its kind changes", async () => {
    const user = userEvent.setup();
    renderEditor();
    await pickApp(user);

    await user.type(screen.getByLabelText("Name"), "x");
    await user.selectOptions(
      screen.getByLabelText("Condition 1 kind"),
      "spike",
    );
    expect(screen.getByLabelText("Condition 1 baseline")).toBeInTheDocument();
    expect(
      screen.queryByLabelText("Condition 1 threshold"),
    ).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Create" }));
    await waitFor(() => expect(createWatch).toHaveBeenCalled());

    const sent = createWatch.mock.calls[0][0] as {
      conditions: { params: Record<string, unknown> }[];
    };
    expect(sent.conditions[0].params.threshold).toBeUndefined();
    expect(sent.conditions[0].params.direction).toBe("up");
  });

  // The bucket width is not on the form: it follows from how often the watch is
  // checked, and is written on every save so a stored width cannot drift from
  // the durations the form showed.
  it("saves the bucket width its check interval implies", async () => {
    const user = userEvent.setup();
    renderEditor();
    await user.type(screen.getByLabelText("Name"), "x");
    await user.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => expect(createWatch).toHaveBeenCalled());
    expect(
      (createWatch.mock.calls[0][0] as { stepSeconds: number }).stepSeconds,
    ).toBe(60);
  });

  // Checking every thirty seconds narrows the buckets, and every window has to
  // keep covering the span it already covered — silently halving them would not
  // show up on a form that displays durations.
  it("keeps window spans when the check interval narrows the bucket", async () => {
    const user = userEvent.setup();
    renderEditor();

    // The default condition is a five-minute window at a one-minute bucket.
    await user.selectOptions(screen.getByLabelText("Check"), "30");
    await user.type(screen.getByLabelText("Name"), "x");
    await user.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => expect(createWatch).toHaveBeenCalled());
    const sent = createWatch.mock.calls[0][0] as {
      stepSeconds: number;
      conditions: { params: Record<string, number> }[];
    };
    expect(sent.stepSeconds).toBe(30);
    expect(sent.conditions[0].params.windowBuckets).toBe(10);
    // And the form still says five minutes.
    expect(
      (screen.getByLabelText("Condition 1 window") as HTMLSelectElement).value,
    ).toBe("300");
  });

  // Scheduling and suppression are different questions and now live apart.
  it("keeps the schedule and the repeat rules in separate sections", async () => {
    const user = userEvent.setup();
    renderEditor();

    await user.selectOptions(screen.getByLabelText("Check"), "300");
    await user.selectOptions(
      screen.getByLabelText("Wait before telling me"),
      "900",
    );
    await user.selectOptions(screen.getByLabelText("Report again"), "3600");
    await user.type(screen.getByLabelText("Name"), "x");
    await user.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => expect(createWatch).toHaveBeenCalled());
    const sent = createWatch.mock.calls[0][0] as Record<string, number>;
    expect(sent.intervalSeconds).toBe(300);
    expect(sent.forSeconds).toBe(900);
    expect(sent.cooldownSeconds).toBe(3600);
  });

  // A watch created over the API can hold a value no preset offers, and a select
  // that dropped it would move it the next time anything else was saved.
  it("keeps a schedule no preset offers", async () => {
    const odd = { ...newWatch(), intervalSeconds: 90, name: "odd" };
    render(<WatchEditor initial={odd} watchId="w_1" />);

    const select = screen.getByLabelText("Check") as HTMLSelectElement;
    expect(select.value).toBe("90");
    expect(screen.getByText("90 seconds")).toBeInTheDocument();
  });

  // A watch that fires and tells nobody is the easiest mistake here, so a new
  // watch has no action and the form says what that means.
  it("starts with no action, and says so", () => {
    renderEditor();
    expect(screen.getByText(/will not tell anybody/i)).toBeInTheDocument();
  });

  it("previews without saving or navigating", async () => {
    const user = userEvent.setup();
    previewWatch.mockResolvedValue({
      status: "ok",
      verdict: "false",
      matched: 0,
      total: 1,
      degraded: false,
      windowFrom: "2026-09-06T09:55:00Z",
      windowTo: "2026-09-06T10:00:00Z",
      outcomes: [
        {
          conditionId: "c_1",
          kind: "threshold",
          label: "error_rate gt over 5m",
          threshold: 0.05,
          observed: 0.01,
          samples: 5,
          windowFrom: "",
          windowTo: "",
          verdict: "false",
          reason: "threshold_unmet",
        },
      ],
    });
    renderEditor();

    await user.click(screen.getByRole("button", { name: /try it now/i }));

    await waitFor(() => expect(previewWatch).toHaveBeenCalled());
    expect(createWatch).not.toHaveBeenCalled();
    expect(push).not.toHaveBeenCalled();
    expect(
      await screen.findByText(/did not reach the threshold/i),
    ).toBeInTheDocument();
  });

  it("surfaces the service's own refusal", async () => {
    const user = userEvent.setup();
    createWatch.mockRejectedValue(
      new Error(
        "alerting: invalid watch: minSamples 9 exceeds the 3-bucket window",
      ),
    );
    renderEditor();

    await user.type(screen.getByLabelText("Name"), "x");
    await user.click(screen.getByRole("button", { name: "Create" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("minSamples 9");
  });

  it("will not create a watch with no name", () => {
    renderEditor();
    expect(screen.getByRole("button", { name: "Create" })).toBeDisabled();
  });

  // The cooldown is the only thing bounding how often a watch reports, so it
  // defaults to a real interval rather than to off — off now means a message on
  // every single check.
  it("sends the report rate, and defaults it to 15 minutes", async () => {
    const user = userEvent.setup();
    renderEditor();

    await user.type(screen.getByLabelText("Name"), "x");
    await user.click(screen.getByRole("button", { name: "Create" }));
    await waitFor(() => expect(createWatch).toHaveBeenCalled());
    expect(
      (createWatch.mock.calls[0][0] as { cooldownSeconds: number })
        .cooldownSeconds,
    ).toBe(900);

    createWatch.mockClear();
    await user.selectOptions(screen.getByLabelText("Report again"), "1800");
    await user.click(screen.getByRole("button", { name: "Create" }));
    await waitFor(() => expect(createWatch).toHaveBeenCalled());
    expect(
      (createWatch.mock.calls[0][0] as { cooldownSeconds: number })
        .cooldownSeconds,
    ).toBe(1800);
  });

  it("saves an existing watch through save rather than create", async () => {
    const user = userEvent.setup();
    renderEditor("w_1");

    await user.type(screen.getByLabelText("Name"), "x");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(saveWatch).toHaveBeenCalled());
    expect(saveWatch.mock.calls[0][0]).toBe("w_1");
    expect(createWatch).not.toHaveBeenCalled();
  });
});
