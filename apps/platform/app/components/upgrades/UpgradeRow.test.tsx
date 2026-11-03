import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import UpgradeRow from "./UpgradeRow";
import type { NamedDeploymentUpgrade } from "@/app/model/upgradeTypes";

const BASE: NamedDeploymentUpgrade = {
  deploymentId: "dep-1",
  integrationId: "int-1",
  integrationName: "Orders",
  name: "Orders",
  status: "running",
  tag: "v1.2.0",
  snapshotId: "snap-1",
  runtimeVersion: "0.11.13",
  upgradeTo: "0.11.14",
  hasSuites: true,
};

function renderRow(
  d: NamedDeploymentUpgrade,
  onTest = vi.fn(),
  onUpgrade = vi.fn(),
) {
  render(
    <UpgradeRow
      d={d}
      runtime="0.11.14"
      busy={false}
      canDeploy
      onTest={onTest}
      onUpgrade={onUpgrade}
    />,
  );
  return { onTest, onUpgrade };
}

describe("UpgradeRow", () => {
  it("tests, and tests then upgrades, a deployment with suites", async () => {
    const { onTest } = renderRow(BASE);
    expect(screen.getByText("Not tested on 0.11.14")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Test" }));
    await userEvent.click(
      screen.getByRole("button", { name: "Test & upgrade" }),
    );
    expect(onTest.mock.calls).toEqual([[false], [true]]);
  });

  it("says how many cases failed on this runtime", () => {
    renderRow({
      ...BASE,
      latestRun: {
        id: "r",
        deploymentId: "dep-1",
        snapshotId: "snap-1",
        runtimeVersion: "0.11.14",
        status: "failed",
        rolloutOnPass: false,
        createdAt: "",
        report: {
          totals: {
            cases: 3,
            passed: 1,
            failed: 2,
            errored: 0,
            skipped: 0,
            notRun: 0,
            elapsedMs: 5,
          },
          suites: [],
        },
      },
    });
    expect(screen.getByText("2 failing cases")).toBeInTheDocument();
  });

  it("offers the editor once a failed run's report is open", async () => {
    renderRow({
      ...BASE,
      latestRun: {
        id: "r",
        deploymentId: "dep-1",
        snapshotId: "snap-1",
        runtimeVersion: "0.11.14",
        status: "errored",
        rolloutOnPass: false,
        createdAt: "",
        error: "a suite or the integration's config could not be run",
      },
    });
    await userEvent.click(
      screen.getByRole("button", { name: /Could not run/ }),
    );
    expect(
      screen.getByRole("link", { name: /Open in the editor/ }),
    ).toHaveAttribute("href", "/platform/i/int-1");
  });

  it("ignores a run on another runtime", () => {
    renderRow({
      ...BASE,
      latestRun: {
        id: "r",
        deploymentId: "dep-1",
        snapshotId: "snap-1",
        runtimeVersion: "0.11.12",
        status: "passed",
        rolloutOnPass: false,
        createdAt: "",
      },
    });
    expect(screen.getByText("Not tested on 0.11.14")).toBeInTheDocument();
  });

  it("calls out a deployment with no tests and offers an untested upgrade", async () => {
    const { onUpgrade } = renderRow({ ...BASE, hasSuites: false });
    expect(screen.getByText("No tests")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Test" })).toBeNull();
    await userEvent.click(
      screen.getByRole("button", { name: /Upgrade untested/ }),
    );
    expect(onUpgrade).toHaveBeenCalled();
  });

  it("sets aside a run older than the last rollout", () => {
    renderRow({
      ...BASE,
      latestRunStale: true,
      latestRun: {
        id: "r",
        deploymentId: "dep-1",
        snapshotId: "snap-1",
        runtimeVersion: "0.11.14",
        status: "passed",
        rolloutOnPass: false,
        createdAt: "",
      },
    });
    expect(screen.queryByText("Passed")).toBeNull();
    expect(
      screen.getByText("Not tested since the last rollout"),
    ).toBeInTheDocument();
  });

  it("offers a pinned deployment a test and a manual move of its pin", async () => {
    const { onTest, onUpgrade } = renderRow({ ...BASE, pinned: true });
    expect(screen.getByText("Pinned")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Test & upgrade/ })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Test" }));
    await userEvent.click(
      screen.getByRole("button", { name: /Move pin to 0.11.14/ }),
    );
    expect(onTest).toHaveBeenCalledWith(false);
    expect(onUpgrade).toHaveBeenCalled();
  });
});
