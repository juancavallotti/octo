import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import TestInputForm from "./TestInputForm";

describe("TestInputForm raw body", () => {
  it("saves a raw body with its content type, verbatim", async () => {
    const onSubmit = vi.fn();
    render(<TestInputForm onSubmit={onSubmit} onCancel={() => {}} />);

    await userEvent.type(screen.getByPlaceholderText("happy path"), "xml order");
    await userEvent.click(screen.getByRole("button", { name: "Raw" }));
    await userEvent.type(screen.getByLabelText("Content type"), "application/xml");
    await userEvent.type(screen.getByLabelText("Raw body"), "<order/> ");
    await userEvent.click(screen.getByRole("button", { name: "Add" }));

    expect(onSubmit).toHaveBeenCalledWith({
      name: "xml order",
      data: "<order/> ",
      contentType: "application/xml",
      vars: undefined,
    });
  });

  it("will not save a raw body without a content type", async () => {
    render(<TestInputForm onSubmit={() => {}} onCancel={() => {}} />);

    await userEvent.type(screen.getByPlaceholderText("happy path"), "xml order");
    await userEvent.click(screen.getByRole("button", { name: "Raw" }));
    await userEvent.type(screen.getByLabelText("Raw body"), "<order/>");

    expect(screen.getByRole("button", { name: "Add" })).toBeDisabled();
  });

  it("opens a saved raw input in raw mode, and switching to JSON drops the type", async () => {
    const onSubmit = vi.fn();
    render(
      <TestInputForm
        initial={{ id: "i", name: "csv", data: "a,b", contentType: "text/csv" }}
        onSubmit={onSubmit}
        onCancel={() => {}}
      />,
    );

    expect(screen.getByLabelText("Content type")).toHaveValue("text/csv");
    expect(screen.getByLabelText("Raw body")).toHaveValue("a,b");

    await userEvent.click(screen.getByRole("button", { name: "JSON" }));
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    // "a,b" is not JSON, so the JSON body refuses it rather than saving it untyped.
    expect(onSubmit).not.toHaveBeenCalled();
  });
});
