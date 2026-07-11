import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import Popover from "./Popover";

function setup(open = true) {
  const onClose = vi.fn();
  const user = userEvent.setup();
  render(
    <div>
      <button>outside</button>
      <Popover
        open={open}
        onClose={onClose}
        trigger={<button>open menu</button>}
      >
        <button>inside</button>
      </Popover>
    </div>,
  );
  return { onClose, user };
}

describe("Popover", () => {
  it("shows its contents only when open", () => {
    setup(false);
    expect(screen.queryByText("inside")).not.toBeInTheDocument();
    expect(screen.getByText("open menu")).toBeInTheDocument();
  });

  it("renders its contents when open", () => {
    setup();
    expect(screen.getByText("inside")).toBeInTheDocument();
  });

  it("closes on a click outside", async () => {
    const { onClose, user } = setup();
    await user.click(screen.getByText("outside"));
    expect(onClose).toHaveBeenCalled();
  });

  // Clicking inside must not dismiss it: the panel is where the user is working.
  it("stays open when clicked inside", async () => {
    const { onClose, user } = setup();
    await user.click(screen.getByText("inside"));
    expect(onClose).not.toHaveBeenCalled();
  });

  it("closes on Escape", async () => {
    const { onClose, user } = setup();
    await user.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalled();
  });

  it("does not listen while closed", async () => {
    const { onClose, user } = setup(false);
    await user.click(screen.getByText("outside"));
    await user.keyboard("{Escape}");
    expect(onClose).not.toHaveBeenCalled();
  });
});
