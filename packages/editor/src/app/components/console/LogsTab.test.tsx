import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import type { RunLogLine } from "../../run/RunContext";
import LogsTab from "./LogsTab";

const line = (seq: number, text: string): RunLogLine => ({ seq, text });

describe("LogsTab", () => {
  it("prompts to press Run when idle with no output", () => {
    render(<LogsTab logs={[]} running={false} />);
    expect(screen.getByText(/press run to start/i)).toBeInTheDocument();
  });

  it("shows a starting spinner once running but before any output arrives", () => {
    const { container } = render(<LogsTab logs={[]} running={true} />);
    // The pod is coming up: reassure the user instead of the resting "press Run" copy.
    expect(screen.getByText(/starting the integration/i)).toBeInTheDocument();
    expect(screen.queryByText(/press run to start/i)).not.toBeInTheDocument();
    expect(container.querySelector(".animate-spin")).not.toBeNull();
  });

  it("drops the spinner and renders lines once output streams in", () => {
    const { container } = render(
      <LogsTab logs={[line(0, "listening on :8080")]} running={true} />,
    );
    expect(screen.getByText("listening on :8080")).toBeInTheDocument();
    expect(screen.queryByText(/starting the integration/i)).not.toBeInTheDocument();
    expect(container.querySelector(".animate-spin")).toBeNull();
  });
});
