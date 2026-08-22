import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";

import UserMessage from "./UserMessage";
import { newTurn, type Delivery, type Turn } from "./turns";

function said(text: string, delivery?: Delivery): Turn {
  return { ...newTurn("u1", "user", text), delivery };
}

describe("UserMessage", () => {
  it("renders what was said as plain text, not as markdown", () => {
    render(<UserMessage turn={said("**not bold**")} />);

    expect(screen.getByText("**not bold**")).toBeTruthy();
  });

  // An ordinary question started its own run, and the answer appearing under it
  // is the acknowledgement. A second one would be noise on every turn.
  it("says nothing about delivery for a question that started a run", () => {
    const { container } = render(<UserMessage turn={said("how many integrations")} />);

    expect(container.querySelectorAll("span")).toHaveLength(0);
  });

  // The request that carried it answered nothing, so between sending and the run
  // picking it up there is no other sign the message is alive.
  it("shows a message sent mid-answer as not yet read", () => {
    const { container } = render(<UserMessage turn={said("and the logs?", "pending")} />);

    expect(screen.getByText(/Waiting for him to read this/)).toBeTruthy();
    expect(container.querySelector(".animate-spin")).toBeTruthy();
  });

  it("clears it once he says he took it in", () => {
    render(<UserMessage turn={said("and the logs?", "taken")} />);

    expect(screen.getByText(/Read, and taken into account/)).toBeTruthy();
    expect(screen.queryByText(/Waiting/)).toBeNull();
  });

  // The one case where something a person sent goes nowhere. It must not be
  // silent, and it must say what to do about it.
  it("says so when he never got to it", () => {
    render(<UserMessage turn={said("and the logs?", "missed")} />);

    expect(screen.getByText(/never got to this one. Ask again/)).toBeTruthy();
  });
});
