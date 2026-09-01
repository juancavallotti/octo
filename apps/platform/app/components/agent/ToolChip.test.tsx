/**
 * The chip is where a gated call is answered, so the question has to be visible
 * without anyone hunting for it — and it arrives after the chip is already on
 * screen, which is the case worth pinning.
 */

import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

import ToolChip from "./ToolChip";
import type { ToolRun } from "./turns";

const call: ToolRun = {
  id: "c1",
  tool: "web_search",
  done: false,
  failed: false,
  input: { objective: "what changed in 1.4" },
};

const asking: ToolRun = {
  ...call,
  authorization: { id: "auth_1", state: "pending", expiresInSeconds: 180 },
};

describe("ToolChip", () => {
  it("shows nothing to answer for an ordinary call", () => {
    render(<ToolChip run={call} onAuthorize={() => {}} />);

    expect(screen.queryByRole("button", { name: "Allow" })).toBeNull();
  });

  // The agent reports the call before it asks about it, so the chip is already
  // rendered and closed when the question lands. Opening only on mount would leave
  // the buttons behind a click nobody knows to make.
  it("opens itself when the question arrives after the call", () => {
    const { rerender } = render(<ToolChip run={call} onAuthorize={() => {}} />);
    expect(screen.queryByRole("button", { name: "Allow" })).toBeNull();

    rerender(<ToolChip run={asking} onAuthorize={() => {}} />);

    expect(screen.getByRole("button", { name: "Allow" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Deny" })).toBeTruthy();
    // The arguments are what is being authorized, so they are on screen with it.
    expect(screen.getByText(/what changed in 1.4/)).toBeTruthy();
  });

  it("answers with the authorization's own id", () => {
    const onAuthorize = vi.fn();
    render(<ToolChip run={asking} onAuthorize={onAuthorize} />);

    fireEvent.click(screen.getByRole("button", { name: "Allow" }));
    expect(onAuthorize).toHaveBeenCalledWith("auth_1", true);

    fireEvent.click(screen.getByRole("button", { name: "Deny" }));
    expect(onAuthorize).toHaveBeenCalledWith("auth_1", false);
  });

  // A decision that has been made is not a question any more, however the call
  // itself went.
  it("stops asking once it has been answered", () => {
    render(
      <ToolChip
        run={{ ...asking, authorization: { id: "auth_1", state: "allowed" } }}
        onAuthorize={() => {}}
      />,
    );

    expect(screen.queryByRole("button", { name: "Allow" })).toBeNull();
  });
});
