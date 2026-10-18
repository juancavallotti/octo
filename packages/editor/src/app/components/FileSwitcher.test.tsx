import { describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EditorActionType, EditorStateProvider, useEditorState } from "../state/editorState";
import { parseProject, type ProjectFile } from "../model/project";
import FileSwitcher from "./FileSwitcher";

const files = parseProject([
  { path: "octo.yaml", content: "service:\n  name: shop\n" },
  { path: "orders.yaml", content: "flows:\n  - name: orders\n    process: []\n" },
]);

const config = `
connectors:
  - name: db
    type: postgres
  - name: api
    type: http
env:
  - name: DB_URL
resources:
  env: [secrets.env]
  templates:
    - resource: welcome.txt
`;

function Harness({ files }: { files: ProjectFile[] }) {
  const { state, dispatch } = useEditorState();
  return (
    <div>
      <button
        onClick={() =>
          dispatch({ type: EditorActionType.LOAD_PROJECT, data: { id: "p", name: "shop", folderId: null, files } })
        }
      >
        load
      </button>
      <FileSwitcher />
      <p data-testid="files">{state.files.map((f) => f.path).join(",")}</p>
      <p data-testid="active">{state.activePath}</p>
    </div>
  );
}

async function setup(project: ProjectFile[] = files) {
  const user = userEvent.setup();
  render(
    <EditorStateProvider>
      <Harness files={project} />
    </EditorStateProvider>,
  );
  await user.click(screen.getByText("load"));
  return user;
}

const active = () => screen.getByTestId("active").textContent;
const paths = () => screen.getByTestId("files").textContent;

describe("FileSwitcher", () => {
  it("names the open file and switches to another", async () => {
    const user = await setup();
    expect(active()).toBe("orders.yaml");
    await user.click(screen.getByRole("button", { name: "Files" }));
    await user.click(screen.getByRole("button", { name: "octo.yaml" }));
    expect(active()).toBe("octo.yaml");
  });

  it("counts what each file declares", async () => {
    const user = await setup(
      parseProject([
        { path: "config.yaml", content: config },
        { path: "orders.yaml", content: "flows:\n  - name: orders\n    process: []\n" },
      ]),
    );
    await user.click(screen.getByRole("button", { name: "Files" }));
    expect(screen.getByRole("button", { name: "config.yaml" })).toHaveAttribute(
      "title",
      "2 connections · 1 env var · 2 resources",
    );
    expect(screen.getByRole("button", { name: "orders.yaml" })).not.toHaveAttribute("title");
  });

  it("adds a file, taking .yaml as the extension when none is typed", async () => {
    const user = await setup();
    await user.click(screen.getByRole("button", { name: "Files" }));
    await user.click(screen.getByRole("button", { name: "New file" }));
    await user.type(screen.getByLabelText("New file name"), "billing{Enter}");
    expect(active()).toBe("billing.yaml");
    expect(paths()).toBe("billing.yaml,octo.yaml,orders.yaml");
  });

  it("explains why a name cannot be used", async () => {
    const user = await setup();
    await user.click(screen.getByRole("button", { name: "Files" }));
    await user.click(screen.getByRole("button", { name: "New file" }));
    await user.type(screen.getByLabelText("New file name"), "octo.yaml");
    expect(screen.getByText("A file with that name already exists.")).toBeInTheDocument();
    await user.clear(screen.getByLabelText("New file name"));
    await user.type(screen.getByLabelText("New file name"), "orders_test");
    expect(screen.getByText("A file ending in _test is a test suite, not config.")).toBeInTheDocument();
  });

  it("renames and removes the open file", async () => {
    const user = await setup();
    await user.click(screen.getByRole("button", { name: "Files" }));
    await user.click(screen.getByRole("button", { name: "Rename orders.yaml" }));
    const input = screen.getByLabelText("File name");
    await user.clear(input);
    await user.type(input, "sales.yaml{Enter}");
    expect(active()).toBe("sales.yaml");

    // The menu stays open on the renamed row.
    await user.click(screen.getByRole("button", { name: "Remove sales.yaml" }));
    expect(paths()).toBe("octo.yaml");
  });

  it("renames the open file in the bar, without opening the menu", async () => {
    const user = await setup();
    await user.click(screen.getByRole("button", { name: "Rename orders.yaml" }));
    expect(screen.queryByRole("button", { name: "New file" })).toBeNull();
    const input = screen.getByLabelText("File name");
    await user.clear(input);
    await user.type(input, "octo{Enter}");
    expect(screen.getByText("A file with that name already exists.")).toBeInTheDocument();
    await user.clear(input);
    await user.type(input, "sales{Enter}");
    expect(active()).toBe("sales.yaml");
    expect(screen.getByRole("button", { name: "Files" })).toHaveTextContent("sales.yaml");
  });

  it("renames a file that is not open in its row, and keeps the open one", async () => {
    const user = await setup();
    await user.click(screen.getByRole("button", { name: "Files" }));
    await user.click(screen.getByRole("button", { name: "Rename octo.yaml" }));
    const input = screen.getByLabelText("File name");
    await user.clear(input);
    await user.type(input, "service{Enter}");
    expect(paths()).toBe("orders.yaml,service.yaml");
    expect(active()).toBe("orders.yaml");
  });

  it("commits a rename on blur, and abandons it on Escape without closing the menu", async () => {
    const user = await setup();
    await user.click(screen.getByRole("button", { name: "Files" }));
    await user.click(screen.getByRole("button", { name: "Rename octo.yaml" }));
    await user.clear(screen.getByLabelText("File name"));
    await user.type(screen.getByLabelText("File name"), "base");
    await user.tab();
    expect(paths()).toBe("base.yaml,orders.yaml");

    await user.click(screen.getByRole("button", { name: "Rename base.yaml" }));
    await user.type(screen.getByLabelText("File name"), "x{Escape}");
    expect(paths()).toBe("base.yaml,orders.yaml");
    expect(screen.getByRole("button", { name: "base.yaml" })).toBeInTheDocument();
  });

  it("settles a rename when the menu is clicked away", async () => {
    const user = await setup();
    await user.click(screen.getByRole("button", { name: "Files" }));
    await user.click(screen.getByRole("button", { name: "Rename octo.yaml" }));
    await user.clear(screen.getByLabelText("File name"));
    await user.type(screen.getByLabelText("File name"), "base");
    await user.click(screen.getByTestId("files"));
    expect(paths()).toBe("base.yaml,orders.yaml");
    expect(screen.queryByLabelText("File name")).toBeNull();
  });

  it("settles a rename and still acts on the row clicked next", async () => {
    const user = await setup();
    await user.click(screen.getByRole("button", { name: "Files" }));
    await user.click(screen.getByRole("button", { name: "Rename octo.yaml" }));
    await user.clear(screen.getByLabelText("File name"));
    await user.type(screen.getByLabelText("File name"), "zeta");
    await user.click(screen.getByRole("button", { name: "Remove orders.yaml" }));
    expect(paths()).toBe("zeta.yaml");
  });

  it("leaves an Escape that ends an IME composition to the input method", async () => {
    const user = await setup();
    await user.click(screen.getByRole("button", { name: "Files" }));
    await user.click(screen.getByRole("button", { name: "Rename octo.yaml" }));
    fireEvent.keyDown(screen.getByLabelText("File name"), { key: "Escape", isComposing: true });
    expect(screen.getByLabelText("File name")).toBeInTheDocument();
  });

  it("closes the menu on Escape from a new file's name", async () => {
    const user = await setup();
    await user.click(screen.getByRole("button", { name: "Files" }));
    await user.click(screen.getByRole("button", { name: "New file" }));
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("button", { name: "New file" })).toBeNull();
  });

  it("removes a file that is not open, and keeps the open one", async () => {
    const user = await setup();
    await user.click(screen.getByRole("button", { name: "Files" }));
    await user.click(screen.getByRole("button", { name: "Remove octo.yaml" }));
    expect(paths()).toBe("orders.yaml");
    expect(active()).toBe("orders.yaml");
  });
});
