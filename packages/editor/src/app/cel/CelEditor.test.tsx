import { describe, it, expect } from "vitest";
import { useState } from "react";
import { render, screen, fireEvent } from "@testing-library/react";
import CelEditor from "./CelEditor";

/** Controlled harness so we can read the value the editor emits. */
function Harness({ initial = "" }: { initial?: string }) {
  const [value, setValue] = useState(initial);
  return (
    <div>
      <CelEditor id="expr" value={value} onChange={setValue} />
      <output data-testid="value">{value}</output>
    </div>
  );
}

function textarea() {
  return screen.getByRole("textbox") as HTMLTextAreaElement;
}

describe("CelEditor autocomplete", () => {
  it("suggests a variable by prefix and inserts it on Enter", () => {
    render(<Harness />);
    const ta = textarea();
    // Type "bo" — react-simple-code-editor emits value via the change event.
    fireEvent.change(ta, { target: { value: "bo", selectionStart: 2 } });

    // The menu offers `body` as an option.
    expect(screen.getByRole("option", { name: /body/ })).toBeInTheDocument();

    // Accept with Enter → the token is replaced with the full name.
    fireEvent.keyDown(ta, { key: "Enter" });
    expect(screen.getByTestId("value").textContent).toBe("body");
  });

  it("appends an opening paren when accepting a function", () => {
    render(<Harness />);
    const ta = textarea();
    fireEvent.change(ta, { target: { value: "start", selectionStart: 5 } });
    expect(screen.getByRole("option", { name: /startsWith/ })).toBeInTheDocument();
    fireEvent.keyDown(ta, { key: "Enter" });
    expect(screen.getByTestId("value").textContent).toBe("startsWith(");
  });

  it("shows no menu after a member dot (basic pass)", () => {
    render(<Harness />);
    const ta = textarea();
    fireEvent.change(ta, { target: { value: "body.na", selectionStart: 7 } });
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  });

  it("dismisses the menu on Escape", () => {
    render(<Harness />);
    const ta = textarea();
    fireEvent.change(ta, { target: { value: "bo", selectionStart: 2 } });
    expect(screen.getByRole("listbox")).toBeInTheDocument();
    fireEvent.keyDown(ta, { key: "Escape" });
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  });
});
