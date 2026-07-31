import { useEffect, type ReactNode } from "react";
import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
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
 * The form half of the Testing tab, driven the way a user drives it: pick a flow, edit a
 * case, then read the file back through the YAML view.
 *
 * Reading it back through the YAML view rather than through the store is the point. The
 * store debounces, but more importantly the file is the artifact — what a form edit is
 * worth is exactly what ends up in `<flow>_test.yaml`, and asserting on the model in
 * between would let a serialization bug through.
 */

function storeWith(content: string): TestSuiteStore {
  return {
    list: async () => (content ? [{ flow: "orders", content }] : []),
    save: async () => {},
    remove: async () => {},
    canEdit: () => true,
  };
}

/** Open the tab on `orders`, with the given suite already stored. */
async function openSuite(content: string) {
  const user = userEvent.setup();

  function Seed({ children }: { children: ReactNode }) {
    const { dispatch } = useEditorState();
    useEffect(() => {
      dispatch({
        type: EditorActionType.LOAD_DOCUMENT,
        data: {
          document: { ...blankDocument(), flows: [{ ...emptyFlow(), name: "orders" }] },
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

  /** The file as it stands, read out of the YAML view and then handed back to the form. */
  const fileNow = async () => {
    await user.click(screen.getByRole("button", { name: "YAML" }));
    const text = (screen.getByRole("textbox") as HTMLTextAreaElement).value;
    await user.click(screen.getByRole("button", { name: "Form" }));
    return text;
  };

  return { user, fileNow };
}

const ONE_CASE = "flow: orders\ncases:\n  - name: it runs\n";

describe("SuiteEditor", () => {
  it("opens on the form, listing the cases", async () => {
    await openSuite("flow: orders\ncases:\n  - name: it runs\n  - name: it charges\n");

    expect(screen.getByRole("button", { name: "Form" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByText("it charges")).toBeInTheDocument();
    // The first case opens with it — there is nothing to choose between yet.
    expect(screen.getByPlaceholderText("it charges the card")).toHaveValue("it runs");
  });

  // The form writes back by re-serializing what it understood, so a file carrying
  // something it did NOT understand would come back with that something deleted — in a
  // file the user is about to commit, and with nothing on screen to show it happened.
  it("refuses the form for a file it could not fully read, and says why", async () => {
    await openSuite("flow: orders\ncases:\n  - name: a\n    spys: {}\n");

    const form = screen.getByRole("button", { name: "Form" });
    expect(form).toBeDisabled();
    expect(form).toHaveAttribute("title", expect.stringContaining("would delete that"));
    expect(screen.getByRole("button", { name: "YAML" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  it("warns that the form drops comments, and offers the YAML view instead", async () => {
    const { user } = await openSuite("# why this matters\nflow: orders\ncases:\n  - name: a\n");

    expect(screen.getByText(/drops its comments/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Switch to YAML" }));

    expect(screen.getByRole("button", { name: "YAML" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  it("says nothing about comments when the file has none", async () => {
    await openSuite(ONE_CASE);
    expect(screen.queryByText(/drops its comments/)).toBeNull();
  });

  // The store is keyed by flow name, so a file that lost its `flow:` is unambiguously
  // wrong rather than ambiguous — and writing it back that way would author a suite
  // dolphin cannot load.
  it("puts back a flow name the file had lost", async () => {
    const { user, fileNow } = await openSuite("cases:\n  - name: a\n");

    await user.clear(screen.getByPlaceholderText("it charges the card"));
    await user.type(screen.getByPlaceholderText("it charges the card"), "b");

    expect(await fileNow()).toContain("flow: orders");
  });
});

describe("CaseForm", () => {
  it("renames a case", async () => {
    const { user, fileNow } = await openSuite(ONE_CASE);

    const name = screen.getByPlaceholderText("it charges the card");
    await user.clear(name);
    await user.type(name, "it settles");

    expect(await fileNow()).toContain("- name: it settles");
  });

  // Two cases with one name is a dolphin LOAD error: the suite does not run one of them
  // twice, it runs none of them — and names nothing in the report to explain why.
  it("refuses a duplicate name, and does not write it", async () => {
    const { user, fileNow } = await openSuite(
      "flow: orders\ncases:\n  - name: a\n  - name: b\n",
    );

    await user.click(screen.getByText("b"));
    const name = screen.getByPlaceholderText("it charges the card");
    await user.clear(name);
    await user.type(name, "a");

    expect(screen.getByText(/Another case is already called that/)).toBeInTheDocument();
    const file = await fileNow();
    expect(file).toContain("- name: b");
  });

  it("says a case needs a name", async () => {
    const { user } = await openSuite(ONE_CASE);

    await user.clear(screen.getByPlaceholderText("it charges the card"));

    expect(screen.getByText(/A case needs a name/)).toBeInTheDocument();
  });

  it("adds a case with a name nothing else has, and opens it", async () => {
    const { user, fileNow } = await openSuite("flow: orders\ncases:\n  - name: new case\n");

    await user.click(screen.getByRole("button", { name: /Add case/ }));

    expect(screen.getByPlaceholderText("it charges the card")).toHaveValue("new case 2");
    expect(await fileNow()).toContain("- name: new case 2");
  });

  // A half-typed field belongs to the case it was typed into. Sliding the next case up
  // into the open pane would silently reattach it to a different test.
  it("closes the detail pane when the case being edited is deleted", async () => {
    const { user } = await openSuite(
      "flow: orders\ncases:\n  - name: a\n  - name: b\n  - name: c\n",
    );

    await user.click(screen.getByText("b"));
    await user.click(screen.getByRole("button", { name: "Delete case 2" }));

    // Deliberately not "c" — which is what now sits at that index. A half-typed field
    // belongs to the case it was typed into.
    expect(screen.getByText("Pick a case")).toBeInTheDocument();
    expect(screen.queryByPlaceholderText("it charges the card")).toBeNull();
  });

  it("stays on the same case when an earlier one is deleted", async () => {
    const { user } = await openSuite("flow: orders\ncases:\n  - name: a\n  - name: b\n");

    await user.click(screen.getByText("b"));
    await user.click(screen.getByRole("button", { name: "Delete case 1" }));

    expect(screen.getByPlaceholderText("it charges the card")).toHaveValue("b");
  });

  it("flags a timeout that is not a Go duration", async () => {
    const { user } = await openSuite(ONE_CASE);

    await user.click(screen.getByText("Environment, skip and timeout"));
    await user.type(screen.getByPlaceholderText("30s"), "3 seconds");

    expect(screen.getByText(/Not a Go duration/)).toBeInTheDocument();
  });
});

describe("CaseExpectField", () => {
  it("writes an exact body", async () => {
    const { user, fileNow } = await openSuite(ONE_CASE);

    await user.type(
      screen.getByPlaceholderText('{ "charged": true }'),
      '{{"ok": true}',
    );

    const file = await fileNow();
    expect(file).toContain("expect:");
    expect(file).toContain("ok: true");
  });

  // dolphin rejects an expectation stating two outcomes: a flow that failed returned no
  // message for a body to be compared against. So the switch drops what it replaces.
  it("switches to dropped and takes the body with it", async () => {
    const { user, fileNow } = await openSuite(
      "flow: orders\ncases:\n  - name: a\n    expect:\n      body: {ok: true}\n",
    );

    await user.click(screen.getByRole("button", { name: "Dropped" }));

    const file = await fileNow();
    expect(file).toContain("dropped: true");
    expect(file).not.toContain("ok: true");
  });

  // An empty `error` and no `error` are the same thing in the file, so the model cannot
  // hold "the user has chosen Error but not said what yet" — the form has to.
  it("keeps the error outcome chosen before anything has been typed", async () => {
    const { user } = await openSuite(ONE_CASE);

    await user.click(screen.getByRole("button", { name: "Error" }));

    expect(screen.getByRole("button", { name: "Error" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByPlaceholderText("card declined")).toBeInTheDocument();
  });

  it("writes an expected error", async () => {
    const { user, fileNow } = await openSuite(ONE_CASE);

    await user.click(screen.getByRole("button", { name: "Error" }));
    await user.type(screen.getByPlaceholderText("card declined"), "declined");

    expect(await fileNow()).toContain("error: declined");
  });

  it("adds a CEL expression, and leaves an empty row out of the file", async () => {
    const { user, fileNow } = await openSuite(ONE_CASE);

    await user.click(screen.getByRole("button", { name: /Add expression/ }));
    await user.type(screen.getByPlaceholderText("body.total > 0"), "body.total > 0");
    await user.click(screen.getByRole("button", { name: /Add expression/ }));

    // Two rows on screen; a half-typed expression is not one dolphin could compile, so
    // only the row that says something reaches the file.
    expect(screen.getAllByPlaceholderText("body.total > 0")).toHaveLength(2);
    const file = await fileNow();
    expect(file.split("body.total > 0").length - 1).toBe(1);
  });
});

describe("JsonValueField", () => {
  // Parsing on every keystroke and storing the result would erase what is being typed the
  // moment an opening brace appears.
  it("does not save half-typed JSON, and does not erase it either", async () => {
    const { user, fileNow } = await openSuite(ONE_CASE);

    const body = screen.getByPlaceholderText('{ "charged": true }');
    await user.type(body, '{{"ok":');

    expect(screen.getByText(/isn't valid JSON yet/)).toBeInTheDocument();
    expect(body).toHaveValue('{"ok":');
    expect(await fileNow()).not.toContain("expect:");
  });

  // `vars` is a map in the format, so valid JSON that is not an object is still a file
  // dolphin refuses — caught in the field rather than as a load error naming the suite.
  it("refuses valid JSON that is not an object where the format wants a map", async () => {
    const { user, fileNow } = await openSuite(ONE_CASE);

    await user.type(screen.getByPlaceholderText('{ "status": "200" }'), "123");

    expect(screen.getByText(/has to be a JSON object/)).toBeInTheDocument();
    expect(await fileNow()).not.toContain("vars:");
  });
});

describe("CaseInputField", () => {
  it("writes an inline body", async () => {
    const { user, fileNow } = await openSuite(ONE_CASE);

    await user.click(screen.getByRole("button", { name: "Inline" }));
    await user.type(screen.getByPlaceholderText('{ "orderId": 42 }'), '{{"orderId": 42}');

    const file = await fileNow();
    expect(file).toContain("input:");
    expect(file).toContain("orderId: 42");
  });

  it("offers a shared input only once the file declares one", async () => {
    await openSuite(ONE_CASE);
    expect(screen.getByRole("button", { name: "Shared" })).toBeDisabled();
  });

  it("picks a declared shared input", async () => {
    const { user, fileNow } = await openSuite(
      "flow: orders\ninputs:\n  a vip order:\n    data: {vip: true}\ncases:\n  - name: it runs\n",
    );

    await user.click(screen.getByRole("button", { name: "Shared" }));

    expect(await fileNow()).toContain("input: a vip order");
  });
});

vi.mock("react-simple-code-editor", () => ({
  // The real editor is a controlled textarea with a highlight layer that needs a DOM
  // measurement jsdom will not do. The placeholder is forwarded because it is how these
  // tests tell one JSON field from another.
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
