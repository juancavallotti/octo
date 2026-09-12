import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";

import { DeploymentPills } from "./DeploymentRowParts";

/**
 * What a deployment may do is a security fact, and these cover the two halves of
 * showing it: that a privileged deployment says so on the card, and that an
 * ordinary one stays quiet — a pill that appeared on everything would stop being
 * read within a day.
 */
describe("the privilege pills on a deployment", () => {
  it("names each grant this deployment's token carries", () => {
    render(<DeploymentPills tag="v1" access={["developer", "operator"]} />);

    expect(screen.getByText("Builds integrations")).toBeInTheDocument();
    expect(screen.getByText("Operates deployments")).toBeInTheDocument();
  });

  it("says the pods carry a shell when they are on the agentic runner", () => {
    render(<DeploymentPills tag="v1" runner="agentic" />);

    expect(screen.getByText("Agentic")).toBeInTheDocument();
  });

  // The ordinary deployment: no grants, the default image. Nothing to say.
  it("says nothing about a deployment holding neither", () => {
    render(<DeploymentPills tag="v1" runner="standard" access={[]} />);

    expect(screen.queryByText("Agentic")).not.toBeInTheDocument();
    expect(screen.queryByText("Builds integrations")).not.toBeInTheDocument();
  });

  // A grant minted by a newer platform than this bundle knows about must still
  // be visible; going quiet about it is the one failure that matters here.
  it("renders a grant it does not recognize under its own name", () => {
    render(
      <DeploymentPills
        tag="v1"
        access={["auditor" as unknown as "developer"]}
      />,
    );

    expect(screen.getByText("auditor")).toBeInTheDocument();
  });
});
