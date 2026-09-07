import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { EmailFields, TopicFields } from "./ActionFields";
import type { AlertAction } from "@/app/model/alerts";
import { NO_TARGET } from "./target";

const email: AlertAction = { id: "a_1", type: "email", params: {} };
const topic: AlertAction = { id: "a_2", type: "topic", params: {} };

describe("address fields", () => {
  // The bug this exists for: parsing on every keystroke and rendering the parsed
  // list back meant the separator vanished as it was typed — `filter(Boolean)`
  // dropped the empty segment after the comma, the value re-rendered without it,
  // and a second address could not be started at all.
  it("lets a comma survive being typed", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<EmailFields action={email} index={0} onChange={onChange} />);

    const input = screen.getByLabelText("Action 1 recipients");
    await user.type(input, "ada@example.com, grace@example.com");

    expect(input).toHaveValue("ada@example.com, grace@example.com");
    const last = onChange.mock.calls.at(-1)?.[0] as AlertAction;
    expect(last.params?.to).toEqual(["ada@example.com", "grace@example.com"]);
  });

  it("keeps the same behaviour for a topic action's report recipients", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <TopicFields
        action={topic}
        index={1}
        target={NO_TARGET}
        destinations={[]}
        onChange={onChange}
      />,
    );

    const input = screen.getByLabelText("Action 2 report recipients");
    await user.type(input, "ada@example.com, ops@example.com");

    expect(input).toHaveValue("ada@example.com, ops@example.com");
    const last = onChange.mock.calls.at(-1)?.[0] as AlertAction;
    expect(last.params?.reportTo).toEqual([
      "ada@example.com",
      "ops@example.com",
    ]);
  });

  // Params arrive from stored JSON with no runtime shape check. A non-array here
  // used to reach `join` and take the whole editor down rather than render.
  it("renders rather than throwing when a stored value is not a list", () => {
    const malformed: AlertAction = {
      id: "a_3",
      type: "email",
      params: { to: "ada@example.com" as unknown as string[] },
    };
    render(<EmailFields action={malformed} index={0} onChange={vi.fn()} />);
    expect(screen.getByLabelText("Action 1 recipients")).toHaveValue("");
  });
});
