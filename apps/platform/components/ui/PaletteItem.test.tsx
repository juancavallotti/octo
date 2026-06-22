import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Webhook } from "lucide-react";
import PaletteItem from "./PaletteItem";

describe("PaletteItem", () => {
  it("renders the label", () => {
    render(<PaletteItem label="Source" icon={Webhook} />);
    expect(screen.getByText("Source")).toBeInTheDocument();
  });

  it("reflects selected state via aria-pressed", () => {
    render(<PaletteItem label="Source" icon={Webhook} selected />);
    expect(screen.getByRole("button")).toHaveAttribute("aria-pressed", "true");
  });

  it("calls onSelect when clicked", async () => {
    const onSelect = vi.fn();
    render(<PaletteItem label="Source" icon={Webhook} onSelect={onSelect} />);
    await userEvent.click(screen.getByRole("button"));
    expect(onSelect).toHaveBeenCalledOnce();
  });
});
