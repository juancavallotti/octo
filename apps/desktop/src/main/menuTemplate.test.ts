import { describe, expect, it, vi } from "vitest";
import type { MenuItemConstructorOptions } from "electron";
import { menuTemplate, type EditorStatus, type MenuActions, type MenuInput } from "./menuTemplate";

const status: EditorStatus = {
  view: "flows",
  flowsView: "canvas",
  hasResources: true,
  hasTests: true,
  editingText: false,
  canUndo: true,
  canRedo: false,
  canSave: true,
  runs: "integration",
  canRun: true,
  canRunFlow: false,
  canStop: false,
  canvas: true,
  panels: { components: true, settings: false, console: false },
};

function actions(): MenuActions {
  return {
    command: vi.fn(),
    pageZoom: vi.fn(),
    pickVault: vi.fn(),
    switchVault: vi.fn(),
    revealVault: vi.fn(),
    copyMcpUrl: vi.fn(),
    openSettings: vi.fn(),
    update: vi.fn(),
    openDocs: vi.fn(),
    showLog: vi.fn(),
  };
}

function build(over: Partial<MenuInput> = {}, act = actions()) {
  const input: MenuInput = {
    isMac: true,
    appName: "Octo",
    status,
    recents: [],
    vaultOpen: true,
    mcpAvailable: true,
    update: { label: "Check for Updates…" },
    ...over,
  };
  return { menu: menuTemplate(input, act), act };
}

/** The item at a path of labels (or roles), e.g. ["File", "New", "Flow"]. */
function item(menu: MenuItemConstructorOptions[], ...path: string[]): MenuItemConstructorOptions {
  let items = menu;
  let found: MenuItemConstructorOptions | undefined;
  for (const name of path) {
    found = items.find((i) => i.label === name || i.role === name);
    if (!found) throw new Error(`no menu item ${path.join(" › ")}`);
    items = (found.submenu as MenuItemConstructorOptions[] | undefined) ?? [];
  }
  return found!;
}

const click = (i: MenuItemConstructorOptions) =>
  (i.click as unknown as () => void)();

describe("menuTemplate", () => {
  it("has the File, Edit, View and Run menus after the app menu", () => {
    const { menu } = build();
    expect(menu.map((m) => m.label ?? m.role)).toEqual([
      "Octo",
      "File",
      "Edit",
      "View",
      "Run",
      "windowMenu",
      "help",
    ]);
  });

  it("enables what the editor says it can do, and nothing else", () => {
    const { menu } = build();
    expect(item(menu, "Edit", "Undo").enabled).toBe(true);
    expect(item(menu, "Edit", "Redo").enabled).toBe(false);
    expect(item(menu, "File", "Save").enabled).toBe(true);
    expect(item(menu, "Run", "Run Integration").enabled).toBe(true);
    expect(item(menu, "Run", "Run Current Flow").enabled).toBe(false);
    expect(item(menu, "Run", "Stop").enabled).toBe(false);
  });

  // In a text field, Undo steps the field, which always has something to offer.
  it("keeps Undo and Redo enabled while text is being edited", () => {
    const { menu } = build({ status: { ...status, editingText: true, canUndo: false } });
    expect(item(menu, "Edit", "Undo").enabled).toBe(true);
    expect(item(menu, "Edit", "Redo").enabled).toBe(true);
  });

  it("forwards a menu item to the editor as its command", () => {
    const { menu, act } = build();
    click(item(menu, "File", "New", "Test Suite"));
    click(item(menu, "View", "Testing"));
    click(item(menu, "Edit", "Undo"));
    expect(act.command).toHaveBeenNthCalledWith(1, "new:suite");
    expect(act.command).toHaveBeenNthCalledWith(2, "view:testing");
    expect(act.command).toHaveBeenNthCalledWith(3, "undo");
  });

  it("checks the view and panels that are showing", () => {
    const { menu } = build();
    expect(item(menu, "View", "Flows").checked).toBe(true);
    expect(item(menu, "View", "Testing").checked).toBe(false);
    expect(item(menu, "View", "Canvas").checked).toBe(true);
    expect(item(menu, "View", "Show Components").checked).toBe(true);
    expect(item(menu, "View", "Show Settings").checked).toBe(false);
  });

  it("names the Testing tab's run for what it runs", () => {
    const { menu } = build({ status: { ...status, view: "testing", runs: "tests", canvas: false } });
    expect(item(menu, "Run", "Run All Tests").accelerator).toBe("CmdOrCtrl+Enter");
  });

  it("offers only the tabs the host backs", () => {
    const { menu } = build({ status: { ...status, hasResources: false } });
    expect(() => item(menu, "View", "Resources")).toThrow();
    expect(item(menu, "File", "New", "Resource…").enabled).toBe(false);
  });

  // The canvas binds the zoom keys while it shows; elsewhere they zoom the page.
  it("zooms the canvas when it shows, and the page otherwise", () => {
    const onCanvas = build();
    click(item(onCanvas.menu, "View", "Zoom Canvas In"));
    expect(onCanvas.act.command).toHaveBeenCalledWith("canvas:zoom-in");

    const elsewhere = build({ status: { ...status, flowsView: "yaml", canvas: false } });
    click(item(elsewhere.menu, "View", "Zoom In"));
    expect(elsewhere.act.pageZoom).toHaveBeenCalledWith("in");
    expect(elsewhere.act.command).not.toHaveBeenCalled();
  });

  // Before the page reports, typing must still undo: the platform's roles stand in.
  it("falls back to the platform's undo and disables editor items before a report", () => {
    const { menu } = build({ status: null });
    expect(item(menu, "Edit", "undo").role).toBe("undo");
    expect(item(menu, "File", "Save").enabled).toBe(false);
    expect(item(menu, "File", "New", "Flow").enabled).toBe(false);
    expect(item(menu, "Run", "Run Integration").enabled).toBe(false);
  });

  it("puts the app items under File off macOS", () => {
    const { menu } = build({ isMac: false });
    expect(menu[0].label).toBe("File");
    expect(item(menu, "File", "Settings…").accelerator).toBe("CmdOrCtrl+,");
  });
});
