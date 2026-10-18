import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
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
      "2 connectors · 1 env var · 2 resources",
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

    await user.click(screen.getByRole("button", { name: "Files" }));
    await user.click(screen.getByRole("button", { name: "Remove sales.yaml" }));
    expect(paths()).toBe("octo.yaml");
  });

  it("removes a file that is not open, and keeps the open one", async () => {
    const user = await setup();
    await user.click(screen.getByRole("button", { name: "Files" }));
    expect(screen.queryByRole("button", { name: "Rename octo.yaml" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "Remove octo.yaml" }));
    expect(paths()).toBe("orders.yaml");
    expect(active()).toBe("orders.yaml");
  });
});
