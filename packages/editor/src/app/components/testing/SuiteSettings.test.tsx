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
 * The suite's file-level settings: the environment every case runs with, the inputs they
 * share, and the timeout that bounds them.
 *
 * Driven through the tab and read back out of the YAML view, like the case form's tests —
 * what a settings edit is worth is exactly what ends up in the committed file.
 */

function storeWith(content: string): TestSuiteStore {
  return {
    list: async () => [{ flow: "orders", content }],
    save: async () => {},
    remove: async () => {},
    canEdit: () => true,
  };
}

async function openSettings(content: string) {
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
  await user.click(screen.getByRole("button", { name: /Suite settings/ }));

  const fileNow = async () => {
    await user.click(screen.getByRole("button", { name: "YAML" }));
    const text = (screen.getByRole("textbox") as HTMLTextAreaElement).value;
    await user.click(screen.getByRole("button", { name: "Form" }));
    return text;
  };

  return { user, fileNow };
}

const ONE_CASE = "flow: orders\ncases:\n  - name: it runs\n";

describe("SuiteSettingsPane", () => {
  // A config's environment resolves when it LOADS, before any block runs, so a flow whose
  // connector reads ${SOME_KEY} cannot be built without one — and mocking the block that
  // uses it does not help. This is what makes the tab able to test a real flow at all.
  it("writes the environment every case runs with", async () => {
    const { user, fileNow } = await openSettings(ONE_CASE);

    await user.click(screen.getByRole("button", { name: "Add variable" }));
    await user.type(screen.getByPlaceholderText("SOME_KEY"), "ANTHROPIC_API_KEY");
    await user.type(screen.getByPlaceholderText("not-a-real-key"), "sk-fake");

    const file = await fileNow();
    expect(file).toContain("env:");
    expect(file).toContain("ANTHROPIC_API_KEY: sk-fake");
  });

  it("says why the environment is needed and why it is safe to commit", async () => {
    await openSettings(ONE_CASE);

    expect(screen.getByText(/before any\s+block runs/)).toBeInTheDocument();
    expect(screen.getByText(/safe to commit/)).toBeInTheDocument();
  });

  // A map cannot hold two entries under one key, so the second is not saved. Doing that
  // silently would leave the user looking at a variable that is not there.
  it("says which duplicate key is not being saved", async () => {
    const { user, fileNow } = await openSettings(
      "flow: orders\nenv:\n  A: one\ncases:\n  - name: it runs\n",
    );

    await user.click(screen.getByRole("button", { name: "Add variable" }));
    const keys = screen.getAllByPlaceholderText("SOME_KEY");
    await user.type(keys[1], "A");
    await user.type(screen.getAllByPlaceholderText("not-a-real-key")[1], "two");

    expect(screen.getByText(/already sets this/)).toBeInTheDocument();
    expect(await fileNow()).toContain("A: one");
  });

  it("removes a variable", async () => {
    const { user, fileNow } = await openSettings(
      "flow: orders\nenv:\n  A: one\ncases:\n  - name: it runs\n",
    );

    await user.click(screen.getByRole("button", { name: "Remove A" }));

    expect(await fileNow()).not.toContain("env:");
  });

  it("flags a timeout that is not a Go duration", async () => {
    const { user } = await openSettings(ONE_CASE);

    await user.type(screen.getByPlaceholderText("30s"), "half a minute");

    expect(screen.getByText(/Not a Go duration/)).toBeInTheDocument();
  });

  it("writes a timeout", async () => {
    const { user, fileNow } = await openSettings(ONE_CASE);

    await user.type(screen.getByPlaceholderText("30s"), "90s");

    expect(await fileNow()).toContain("timeout: 90s");
  });

  // Changing it here would orphan the file from the flow it tests — the store keys by
  // flow name — so it is shown and not edited.
  it("shows the flow under test without offering to change it", async () => {
    await openSettings(ONE_CASE);

    expect(screen.getByText("Flow under test")).toBeInTheDocument();
    expect(screen.queryByDisplayValue("orders")).toBeNull();
  });

  it("marks a file-level problem on the row that opens it", async () => {
    await openSettings("flow: orders\ntimeout: soon\ncases:\n  - name: it runs\n");

    const row = screen.getByRole("button", { name: /Suite settings/ });
    expect(within(row).getByText("1")).toBeInTheDocument();
  });
});

describe("SuiteInputsField", () => {
  it("declares a shared input, which a case can then be given", async () => {
    const { user, fileNow } = await openSettings(ONE_CASE);

    await user.click(screen.getByRole("button", { name: "Add shared input" }));
    await user.type(screen.getByPlaceholderText("a vip order"), "a vip order");
    await user.type(screen.getByPlaceholderText('{ "orderId": 42 }'), '{{"vip": true}');

    const file = await fileNow();
    expect(file).toContain("inputs:");
    expect(file).toContain("a vip order:");
    expect(file).toContain("vip: true");

    // And the case form's shared picker, dead until now, has something to offer.
    await user.click(screen.getByText("it runs"));
    await user.click(screen.getByRole("button", { name: "Shared" }));
    expect(await fileNow()).toContain("input: a vip order");
  });

  // The JSON fields inside a row hold their own draft text, so a row has to have an
  // identity of its own. Keyed by position, deleting one would leave the row below
  // wearing the deleted row's body.
  it("does not leave a deleted input's body behind on the next one", async () => {
    const { user } = await openSettings(
      "flow: orders\ninputs:\n  first:\n    data: {a: 1}\n  second:\n    data: {b: 2}\ncases:\n  - name: it runs\n",
    );

    await user.click(screen.getByRole("button", { name: "Remove input first" }));

    const body = screen.getByPlaceholderText('{ "orderId": 42 }') as HTMLTextAreaElement;
    expect(body.value).toContain('"b"');
    expect(body.value).not.toContain('"a"');
  });

  it("removes a shared input", async () => {
    const { user, fileNow } = await openSettings(
      "flow: orders\ninputs:\n  a vip order:\n    data: {vip: true}\ncases:\n  - name: it runs\n",
    );

    await user.click(screen.getByRole("button", { name: "Remove input a vip order" }));

    expect(await fileNow()).not.toContain("inputs:");
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
