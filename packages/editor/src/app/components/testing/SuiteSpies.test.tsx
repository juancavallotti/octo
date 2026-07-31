import { useEffect, type ReactNode } from "react";
import { describe, it, expect, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  EditorStateProvider,
  useEditorState,
  EditorActionType,
} from "../../state/editorState";
import {
  TestSuiteProvider,
  type TestSuiteStore,
} from "../../providers/TestSuiteProvider";
import { blankDocument, emptyFlow } from "../../model/document";
import TestingView from "./TestingView";

/**
 * Watching a block, and saying what it should have seen.
 *
 * The distinction these tests exist to hold is `count`: absent means the number of
 * crossings is not asserted, `0` asserts the block never ran. They are different
 * assertions and the form must never turn one into the other.
 */

function storeWith(content: string): TestSuiteStore {
  return {
    list: async () => [{ flow: "orders", content }],
    save: async () => {},
    remove: async () => {},
    canEdit: () => true,
  };
}

async function openCase(content: string) {
  const user = userEvent.setup();

  function Seed({ children }: { children: ReactNode }) {
    const { dispatch } = useEditorState();
    useEffect(() => {
      dispatch({
        type: EditorActionType.LOAD_DOCUMENT,
        data: {
          document: {
            ...blankDocument(),
            flows: [
              {
                ...emptyFlow(),
                name: "orders",
                process: [
                  { id: "b1", type: "http-call", name: "charge", settings: {} },
                  { id: "b2", type: "log", name: "audit", settings: {} },
                ],
              },
            ],
          },
        },
      });
      dispatch({ type: EditorActionType.SET_INTEGRATION_ID, data: { id: "one" } });
    }, [dispatch]);
    return <>{children}</>;
  }

  render(
    <EditorStateProvider>
      <Seed>
        <TestSuiteProvider store={storeWith(content)}>
          <TestingView />
        </TestSuiteProvider>
      </Seed>
    </EditorStateProvider>,
  );

  await user.click(await screen.findByRole("button", { name: "orders" }));
  await user.click(screen.getByText("it runs"));

  const fileNow = async () => {
    await user.click(screen.getByRole("button", { name: "YAML" }));
    const text = (screen.getByRole("textbox") as HTMLTextAreaElement).value;
    await user.click(screen.getByRole("button", { name: "Form" }));
    return text;
  };

  return { user, fileNow };
}

const ONE_CASE = "flow: orders\ncases:\n  - name: it runs\n";
const WATCHING = "flow: orders\ncases:\n  - name: it runs\n    spies:\n      orders.charge: {}\n";

describe("SpiesField", () => {
  // Watching is what makes a block's crossings appear in the report at all, so an entry
  // that asserts nothing is still worth writing — "show me what went through here".
  it("watches a block without asserting anything", async () => {
    const { user, fileNow } = await openCase(ONE_CASE);

    await user.selectOptions(
      screen.getByRole("combobox", { name: "Watch a block" }),
      "orders.charge",
    );

    const file = await fileNow();
    expect(file).toContain("spies:");
    expect(file).toContain("orders.charge: {}");
  });

  // The one that has to be right. `count: 0` asserts the block never ran — one of the
  // more useful things a test can say — and a falsy check anywhere would drop it.
  it("keeps a count of zero, which asserts the block never ran", async () => {
    const { user, fileNow } = await openCase(WATCHING);

    await user.type(screen.getByPlaceholderText("not asserted"), "0");

    expect(screen.getByText("asserts the block never ran")).toBeInTheDocument();
    expect(await fileNow()).toContain("count: 0");
  });

  it("normalizes a directly typed negative count to zero", async () => {
    const { user, fileNow } = await openCase(WATCHING);

    await user.type(screen.getByPlaceholderText("not asserted"), "-1");

    const file = await fileNow();
    expect(file).toContain("count: 0");
    expect(file).not.toContain("count: -1");
  });

  it("leaves the count out when the box is empty", async () => {
    const { user, fileNow } = await openCase(
      "flow: orders\ncases:\n  - name: it runs\n    spies:\n      orders.charge:\n        count: 2\n",
    );

    await user.clear(screen.getByPlaceholderText("not asserted"));

    expect(await fileNow()).not.toContain("count:");
  });

  it("stops watching a block", async () => {
    const { user, fileNow } = await openCase(WATCHING);

    await user.click(screen.getByRole("button", { name: "Stop watching orders.charge" }));

    expect(await fileNow()).not.toContain("spies:");
  });

  it("keeps an address no block answers to, and flags it", async () => {
    await openCase(
      "flow: orders\ncases:\n  - name: it runs\n    spies:\n      orders.gone: {}\n",
    );

    expect(screen.getByText("orders.gone")).toBeInTheDocument();
    expect(screen.getByText("unknown")).toBeInTheDocument();
  });
});

describe("RecordField", () => {
  it("asserts what one crossing produced", async () => {
    const { user, fileNow } = await openCase(WATCHING);

    // Take the case's own expectation off the Message outcome first, so the only body
    // field on screen is the crossing's.
    await user.click(screen.getAllByRole("button", { name: "Dropped" })[0]);
    await user.click(screen.getByRole("button", { name: /Assert a crossing/ }));
    await user.type(screen.getByPlaceholderText('{ "charged": true }'), '{{"ok": true}');

    const file = await fileNow();
    expect(file).toContain("records:");
    expect(file).toContain("output:");
    expect(file).toContain("ok: true");
  });

  // The spy clones the message BEFORE the block runs, so a block that failed still shows
  // what it was carrying — which is the whole reason to spy on one you mocked into
  // failing. So `input` is not part of the three-way switch.
  it("still asserts what went in when the block failed", async () => {
    const { user, fileNow } = await openCase(WATCHING);

    await user.click(screen.getAllByRole("button", { name: "Dropped" })[0]);
    await user.click(screen.getByRole("button", { name: /Assert a crossing/ }));
    // Written BEFORE the switch, so the switch has something to lose: the three ways out
    // are exclusive, but the input is not one of them and must survive the change.
    await user.type(screen.getByPlaceholderText('{ "orderId": 42 }'), '{{"id": 1}');
    const crossing = within(screen.getByRole("group", { name: "Crossing outcome" }));
    await user.click(crossing.getByRole("button", { name: "Error" }));
    await user.type(screen.getByPlaceholderText("card declined"), "declined");

    const file = await fileNow();
    expect(file).toContain("error: declined");
    expect(file).toContain("input:");
    expect(file).toContain("id: 1");
  });

  // Each crossing's fields hold their own drafts, so a crossing needs an identity that
  // deleting the one above it does not hand over.
  it("does not leave a deleted crossing's assertion on the next one", async () => {
    const { user } = await openCase(
      "flow: orders\ncases:\n  - name: it runs\n    expect:\n      dropped: true\n    spies:\n      orders.charge:\n        records:\n          - output: {body: {a: 1}}\n          - output: {body: {b: 2}}\n",
    );

    await user.click(screen.getByRole("button", { name: "Remove crossing 1" }));

    const bodies = screen.getAllByPlaceholderText(
      '{ "charged": true }',
    ) as HTMLTextAreaElement[];
    expect(bodies).toHaveLength(1);
    expect(bodies[0].value).toContain('"b"');
    expect(bodies[0].value).not.toContain('"a"');
  });
});

vi.mock("react-simple-code-editor", () => ({
  default: ({
    value,
    onValueChange,
    placeholder,
    readOnly,
  }: {
    value: string;
    onValueChange: (v: string) => void;
    placeholder?: string;
    readOnly?: boolean;
  }) => (
    <textarea
      value={value}
      placeholder={placeholder}
      readOnly={readOnly}
      onChange={(e) => onValueChange(e.target.value)}
    />
  ),
}));
