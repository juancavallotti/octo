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
import type { EditorDocument } from "../../model/document";
import TestingView from "./TestingView";

/**
 * Standing a block in for something else, from both levels of the file.
 *
 * The document carries two named blocks so they have addresses: the runner selects a
 * block by its name, else its type, and an unnamed one among peers of the same type has
 * no unambiguous address at all — which is exactly why the form picks addresses from a
 * list rather than taking them as text.
 */

function docWithBlocks(): EditorDocument {
  return {
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
  };
}

function storeWith(content: string): TestSuiteStore {
  return {
    list: async () => [{ flow: "orders", content }],
    save: async () => {},
    remove: async () => {},
    canEdit: () => true,
  };
}

async function open(content: string, where: "settings" | string) {
  const user = userEvent.setup();

  function Seed({ children }: { children: ReactNode }) {
    const { dispatch } = useEditorState();
    useEffect(() => {
      dispatch({
        type: EditorActionType.LOAD_DOCUMENT,
        data: { document: docWithBlocks() },
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
  await user.click(
    screen.getByRole("button", {
      name: where === "settings" ? /Suite settings/ : where,
    }),
  );

  const fileNow = async () => {
    await user.click(screen.getByRole("button", { name: "YAML" }));
    const text = (screen.getByRole("textbox") as HTMLTextAreaElement).value;
    await user.click(screen.getByRole("button", { name: "Form" }));
    return text;
  };

  return { user, fileNow };
}

const ONE_CASE = "flow: orders\ncases:\n  - name: it runs\n";
const FILE_MOCKS =
  "flow: orders\nmocks:\n  orders.charge:\n    default:\n      body: {ok: true}\ncases:\n  - name: it runs\n";

describe("AddressPicker", () => {
  // An address typed by hand is a spelling test whose failure mode is a suite that loads
  // fine and stands in for nothing.
  it("offers the addresses the runner knows, and nothing else", async () => {
    await open(ONE_CASE, "settings");

    const picker = screen.getByRole("combobox", { name: "Mock a block" });
    expect(
      [...picker.querySelectorAll("option")].map((o) => o.getAttribute("value")),
    ).toEqual(["", "orders.charge", "orders.audit"]);
  });

  it("stops offering an address once it has been mocked", async () => {
    const { user } = await open(ONE_CASE, "settings");

    await user.selectOptions(
      screen.getByRole("combobox", { name: "Mock a block" }),
      "orders.charge",
    );

    const picker = screen.getByRole("combobox", { name: "Mock a block" });
    expect(
      [...picker.querySelectorAll("option")].map((o) => o.getAttribute("value")),
    ).toEqual(["", "orders.audit"]);
  });

  it("says why there is nothing to pick when no block can be addressed", async () => {
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
          <TestSuiteProvider store={storeWith(ONE_CASE)}>
            <TestingView />
          </TestSuiteProvider>
        </Seed>
      </EditorStateProvider>,
    );
    await user.click(await screen.findByRole("button", { name: "orders" }));
    await user.click(screen.getByRole("button", { name: /Suite settings/ }));

    expect(screen.getByText(/Give one a name/)).toBeInTheDocument();
  });
});

describe("MocksField", () => {
  // A mock REPLACES the block, so a message matching no case does not fall through to the
  // real one — it fails. Starting with a default rather than an empty spec means the
  // first mock anyone writes is one that works.
  it("starts a new mock with a default", async () => {
    const { user, fileNow } = await open(ONE_CASE, "settings");

    await user.selectOptions(
      screen.getByRole("combobox", { name: "Mock a block" }),
      "orders.charge",
    );
    await user.type(screen.getByPlaceholderText('{ "charged": true }'), '{{"ok": true}');

    const file = await fileNow();
    expect(file).toContain("orders.charge:");
    expect(file).toContain("default:");
    expect(file).toContain("ok: true");
  });

  it("warns where the default would go when there isn't one", async () => {
    const { user } = await open(ONE_CASE, "settings");

    await user.selectOptions(
      screen.getByRole("combobox", { name: "Mock a block" }),
      "orders.charge",
    );
    await user.click(screen.getByRole("button", { name: "Remove default" }));

    expect(screen.getByText(/no real one left to fall through to/)).toBeInTheDocument();
  });

  it("adds a conditional case with its own outcome", async () => {
    const { user, fileNow } = await open(ONE_CASE, "settings");

    await user.selectOptions(
      screen.getByRole("combobox", { name: "Mock a block" }),
      "orders.charge",
    );
    await user.click(screen.getByRole("button", { name: /Add mock case/ }));
    await user.type(screen.getByPlaceholderText("body.amount > 100"), "body.amount > 100");
    await user.click(screen.getAllByRole("button", { name: "Error" })[0]);
    await user.type(screen.getByPlaceholderText("card declined"), "declined");

    const file = await fileNow();
    expect(file).toContain("when: body.amount > 100");
    expect(file).toContain("error: declined");
  });

  it("removes a mock", async () => {
    const { user, fileNow } = await open(FILE_MOCKS, "settings");

    await user.click(screen.getByRole("button", { name: "Remove mock for orders.charge" }));

    expect(await fileNow()).not.toContain("mocks:");
  });

  // Deciding for the user that a renamed block means the mock is rubbish would be the
  // same silent edit the picker exists to avoid.
  it("keeps an address no block answers to, and flags it", async () => {
    await open(
      "flow: orders\nmocks:\n  orders.gone:\n    default:\n      drop: true\ncases:\n  - name: it runs\n",
      "settings",
    );

    expect(screen.getByText("orders.gone")).toBeInTheDocument();
    expect(screen.getByText("unknown")).toBeInTheDocument();
  });
});

describe("case-level mocks", () => {
  // dolphin's rule (File.MocksFor) is whole-spec replacement per address, not a merge. A
  // form that implied otherwise would give the canvas a different run than `dolphin test`.
  it("says a case's mock replaces the file's entirely", async () => {
    const { user } = await open(FILE_MOCKS, "it runs");

    await user.selectOptions(
      screen.getByRole("combobox", { name: "Mock a block" }),
      "orders.charge",
    );

    expect(screen.getByText(/Replaces the file's mock/)).toBeInTheDocument();
  });

  it("writes the mock under the case, not the file", async () => {
    const { user, fileNow } = await open(ONE_CASE, "it runs");

    await user.selectOptions(
      screen.getByRole("combobox", { name: "Mock a block" }),
      "orders.audit",
    );
    await user.click(screen.getAllByRole("button", { name: "Drop" })[0]);

    const file = await fileNow();
    expect(file).toMatch(/cases:[\s\S]*orders\.audit:/);
    expect(file).not.toMatch(/^mocks:/m);
  });

  // The un-mock: one case runs the real block while the file keeps standing it in for
  // every other case. Written as a null on the address, which is what dolphin reads.
  it("lifts a file mock for one case, and writes it as a null", async () => {
    const { user, fileNow } = await open(FILE_MOCKS, "it runs");

    await user.selectOptions(
      screen.getByRole("combobox", { name: "Run the real block" }),
      "orders.charge",
    );

    expect(screen.getByText(/The real block runs in this case/)).toBeInTheDocument();
    expect(await fileNow()).toMatch(/cases:[\s\S]*orders\.charge: null/);
  });

  // Only the file's mocks can be lifted: un-mocking anything else does nothing, so
  // dolphin refuses it rather than letting it read as "the real block runs here".
  it("offers only the addresses the file mocks, and stops once one is taken", async () => {
    const { user } = await open(FILE_MOCKS, "it runs");

    const offered = () =>
      [
        ...screen
          .getByRole("combobox", { name: "Run the real block" })
          .querySelectorAll("option"),
      ].map((o) => o.getAttribute("value"));

    // orders.audit is on the canvas but not mocked by the file, so it is not liftable.
    expect(offered()).toEqual(["", "orders.charge"]);

    await user.selectOptions(
      screen.getByRole("combobox", { name: "Run the real block" }),
      "orders.charge",
    );
    expect(
      screen.queryByRole("combobox", { name: "Run the real block" }),
    ).not.toBeInTheDocument();
  });

  it("has nothing to lift when the file mocks nothing", async () => {
    await open(ONE_CASE, "it runs");

    expect(
      screen.queryByRole("combobox", { name: "Run the real block" }),
    ).not.toBeInTheDocument();
  });

  it("goes back to inheriting the file's mock when the un-mock is removed", async () => {
    const { user, fileNow } = await open(FILE_MOCKS, "it runs");

    await user.selectOptions(
      screen.getByRole("combobox", { name: "Run the real block" }),
      "orders.charge",
    );
    await user.click(
      screen.getByRole("button", { name: "Restore the file's mock for orders.charge" }),
    );

    const file = await fileNow();
    expect(file).not.toContain("null");
    expect(file).toMatch(/^mocks:/m); // the file's own mock is untouched
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
