/**
 * The chip is where a gated call is answered, so the question has to be visible
 * without anyone hunting for it — and it arrives after the chip is already on
 * screen, which is the case worth pinning.
 */

import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";

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
    render(<ToolChip run={call} onAuthorize={async () => true} />);

    expect(screen.queryByRole("button", { name: "Allow" })).toBeNull();
  });

  // The agent reports the call before it asks about it, so the chip is already
  // rendered and closed when the question lands. Opening only on mount would leave
  // the buttons behind a click nobody knows to make.
  it("opens itself when the question arrives after the call", () => {
    const { rerender } = render(<ToolChip run={call} onAuthorize={async () => true} />);
    expect(screen.queryByRole("button", { name: "Allow" })).toBeNull();

    rerender(<ToolChip run={asking} onAuthorize={async () => true} />);

    expect(screen.getByRole("button", { name: "Allow" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Deny" })).toBeTruthy();
    // The arguments are what is being authorized, so they are on screen with it.
    expect(screen.getByText(/what changed in 1.4/)).toBeTruthy();
  });

  it("answers with the authorization's own id", () => {
    const onAuthorize = vi.fn(async () => true);
    render(<ToolChip run={asking} onAuthorize={onAuthorize} />);

    fireEvent.click(screen.getByRole("button", { name: "Allow" }));
    expect(onAuthorize).toHaveBeenCalledWith("auth_1", true);
  });

  it("can be denied as well as allowed", () => {
    const onAuthorize = vi.fn(async () => true);
    render(<ToolChip run={asking} onAuthorize={onAuthorize} />);

    fireEvent.click(screen.getByRole("button", { name: "Deny" }));
    expect(onAuthorize).toHaveBeenCalledWith("auth_1", false);
  });

  // The run deletes the gate as it takes the first answer, so a second click
  // reaches nothing and is discarded in silence. Someone correcting a mis-click
  // would watch the search run anyway with no sign their correction was dropped —
  // so the choice stops being offered once it has been made.
  it("stops offering a choice that has already been made", async () => {
    const onAuthorize = vi.fn(async () => true);
    render(<ToolChip run={asking} onAuthorize={onAuthorize} />);

    fireEvent.click(screen.getByRole("button", { name: "Allow" }));

    expect(screen.queryByRole("button", { name: "Deny" })).toBeNull();
    expect(screen.getByText(/Allowed/)).toBeTruthy();
    await waitFor(() => expect(onAuthorize).toHaveBeenCalledTimes(1));
  });

  // An answer that never reached the run and one never given look identical from
  // the reader's chair: silence, then a denial on the timeout. So a failed send
  // gives the buttons back and says so.
  it("gives the buttons back when the answer did not reach him", async () => {
    render(<ToolChip run={asking} onAuthorize={async () => false} />);

    fireEvent.click(screen.getByRole("button", { name: "Allow" }));

    await waitFor(() => expect(screen.getByText(/did not reach him/)).toBeTruthy());
    expect(screen.getByRole("button", { name: "Allow" })).toBeTruthy();
  });

  // Rounding to the nearest minute reads "0 min" for a 20-second wait — a call
  // claimed to have expired while its buttons are still live.
  it("says a sub-minute wait in seconds", () => {
    render(
      <ToolChip
        run={{ ...asking, authorization: { id: "auth_1", state: "pending", expiresInSeconds: 20 } }}
        onAuthorize={async () => true}
      />,
    );

    expect(screen.getByText(/in 20s/)).toBeTruthy();
    expect(screen.queryByText(/0 min/)).toBeNull();
  });

  // A decision that has been made is not a question any more, however the call
  // itself went.
  it("stops asking once it has been answered", () => {
    render(
      <ToolChip
        run={{ ...asking, authorization: { id: "auth_1", state: "allowed" } }}
        onAuthorize={async () => true}
      />,
    );

    expect(screen.queryByRole("button", { name: "Allow" })).toBeNull();
  });
});
