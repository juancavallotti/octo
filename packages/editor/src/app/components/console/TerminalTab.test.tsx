import { describe, it, expect, vi } from "vitest";
import { render, screen, act, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ConsoleProvider, useConsole } from "../../run/console";
import {
  TerminalProvider,
  type TerminalSession,
  type TerminalTransport,
} from "../../providers/TerminalProvider";
import { EditorStateProvider } from "../../state/editorState";
import { CelTesterProvider } from "../../cel/CelTesterStore";
import { RunProvider } from "../../run/RunContext";
import type { RunTransport } from "../../run/transport";
import LogPanel from "../LogPanel";
import ConsoleTabs from "./ConsoleTabs";
import TerminalTab from "./TerminalTab";

/** A runner whose binary will not run: the console has nothing of its own to show. */
const notRunning = {
  available: false,
  running: false,
  version: null,
  testAvailable: false,
  testVersion: null,
  testUrl: null,
  exposable: false,
  reloadsOnSave: false,
};
const unavailable: RunTransport = {
  status: async () => notRunning,
  start: async () => notRunning,
  stop: async () => {},
  sync: async () => {},
  invoke: async () => ({ ok: true, dropped: false, timedOut: false, output: "", logs: [] }),
  evalCel: async () => ({ ok: true }),
  subscribeLogs: () => () => {},
  test: async () => {
    throw new Error("not available");
  },
};

// xterm draws on a canvas jsdom does not have; the tab's behaviour is in which
// sessions exist, not in how a screen paints.
vi.mock("../../terminal/screen", () => ({
  isTerminalShortcut: () => false,
  createScreen: async () => ({
    mount: (viewport: HTMLElement) => viewport.replaceChildren(document.createElement("div")),
    unmount: () => {},
    fit: () => {},
    focus: () => {},
    focused: () => false,
    clear: () => {},
    write: () => {},
    onInput: () => () => {},
    onResize: () => () => {},
    dispose: () => {},
  }),
}));

interface FakeSession extends TerminalSession {
  killed: boolean;
  exit(code: number): void;
}

function fakeTransport() {
  const sessions: FakeSession[] = [];
  const transport: TerminalTransport = {
    async open() {
      const exits = new Set<(code: number) => void>();
      const session: FakeSession = {
        id: `t${sessions.length + 1}`,
        title: "zsh",
        killed: false,
        write: () => {},
        resize: () => {},
        kill() {
          session.killed = true;
        },
        onData: () => () => {},
        onExit(listener) {
          exits.add(listener);
          return () => exits.delete(listener);
        },
        exit: (code) => exits.forEach((l) => l(code)),
      };
      sessions.push(session);
      return session;
    },
  };
  return { transport, sessions };
}

function Strip() {
  const { tab, setTab } = useConsole();
  return <ConsoleTabs active={tab} onSelect={setTab} />;
}

describe("Terminal tab", () => {
  it("is not offered when the host supplies no terminal", () => {
    render(
      <ConsoleProvider>
        <Strip />
      </ConsoleProvider>,
    );
    expect(screen.queryByRole("button", { name: /terminal/i })).toBeNull();
    expect(screen.getByRole("button", { name: /logs/i })).toBeTruthy();
  });

  it("is offered when it does", () => {
    const { transport } = fakeTransport();
    render(
      <ConsoleProvider>
        <TerminalProvider value={transport}>
          <Strip />
        </TerminalProvider>
      </ConsoleProvider>,
    );
    expect(screen.getByRole("button", { name: /terminal/i })).toBeTruthy();
  });

  it("starts a shell on arrival, adds more with +, and kills the one closed", async () => {
    const { transport, sessions } = fakeTransport();
    const user = userEvent.setup();
    render(
      <TerminalProvider value={transport}>
        <TerminalTab />
      </TerminalProvider>,
    );
    const list = await screen.findByRole("list", { name: "Terminals" });
    await within(list).findAllByText("zsh");
    expect(sessions).toHaveLength(1);

    await user.click(screen.getByRole("button", { name: "New terminal" }));
    expect(await within(list).findAllByText("zsh")).toHaveLength(2);

    await user.click(within(list).getAllByRole("button", { name: "Kill zsh" })[0]);
    expect(within(list).getAllByText("zsh")).toHaveLength(1);
    expect(sessions[0].killed).toBe(true);
    expect(sessions[1].killed).toBe(false);
  });

  it("marks a shell that exits on its own, and keeps its row", async () => {
    const { transport, sessions } = fakeTransport();
    render(
      <TerminalProvider value={transport}>
        <TerminalTab />
      </TerminalProvider>,
    );
    await screen.findByText("zsh");
    act(() => sessions[0].exit(0));
    expect(screen.getByText("exited")).toBeTruthy();
    expect(screen.getByText("zsh")).toBeTruthy();
  });

  it("kills a shell that finishes starting after the editor has gone", async () => {
    const { transport, sessions } = fakeTransport();
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    const slow: TerminalTransport = {
      open: async (size) => {
        await gate;
        return transport.open(size);
      },
    };
    const { unmount } = render(
      <TerminalProvider value={slow}>
        <TerminalTab />
      </TerminalProvider>,
    );
    unmount();
    await act(async () => release());
    await vi.waitFor(() => expect(sessions[0]?.killed).toBe(true));
  });

  it("stays in the console when the runner cannot run, alone", async () => {
    const { transport } = fakeTransport();
    render(
      <EditorStateProvider>
        <ConsoleProvider>
          <CelTesterProvider>
            <RunProvider transport={unavailable}>
              <TerminalProvider value={transport}>
                <LogPanel />
              </TerminalProvider>
            </RunProvider>
          </CelTesterProvider>
        </ConsoleProvider>
      </EditorStateProvider>,
    );
    expect(await screen.findByRole("button", { name: "Terminal" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Logs" })).toBeNull();
  });

  it("reports a shell that would not start beside the list, not over a live one", async () => {
    const { transport } = fakeTransport();
    let calls = 0;
    const flaky: TerminalTransport = {
      open: async (size) => {
        if (++calls > 1) throw new Error("no such folder");
        return transport.open(size);
      },
    };
    const user = userEvent.setup();
    render(
      <TerminalProvider value={flaky}>
        <TerminalTab />
      </TerminalProvider>,
    );
    await screen.findByText("zsh");
    await user.click(screen.getByRole("button", { name: "New terminal" }));
    const alert = await screen.findByRole("alert");
    expect(alert.getAttribute("title")).toContain("no such folder");
    expect(screen.queryByText(/Could not start a terminal: no such folder/)).toBeNull();
  });

  it("kills every shell when the editor goes away", async () => {
    const { transport, sessions } = fakeTransport();
    const { unmount } = render(
      <TerminalProvider value={transport}>
        <TerminalTab />
      </TerminalProvider>,
    );
    await screen.findByText("zsh");
    unmount();
    expect(sessions[0].killed).toBe(true);
  });
});
