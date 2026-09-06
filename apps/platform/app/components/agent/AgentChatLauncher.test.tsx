/**
 * The shell around every signed-in page: what it renders when the agent is not
 * deployed, and the one property the whole design turns on — that the drawer is
 * never re-parented. Collapsing the panel or pinning it must not remount the
 * drawer, because a remount aborts the fetch streaming an answer into it and takes
 * the conversation with it.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import AgentChatLauncher from "./AgentChatLauncher";

/**
 * A stand-in for the real drawer: it reports the props the shell drives it with,
 * and offers the two callbacks the shell listens to.
 */
vi.mock("./AgentDrawer", () => ({
  default: ({
    docked,
    width,
    onCollapse,
    onToggleDock,
    onBusy,
  }: {
    docked: boolean;
    width: number;
    onCollapse: () => void;
    onToggleDock: () => void;
    onBusy: (busy: boolean) => void;
  }) => (
    <div data-testid="drawer" data-docked={String(docked)} data-width={width}>
      <button onClick={onCollapse}>collapse</button>
      <button onClick={onToggleDock}>pin</button>
      <button onClick={() => onBusy(true)}>work</button>
    </div>
  ),
}));

/**
 * Stub the availability probe. Hands back a promise that settles once the body has
 * actually been read, so a test can assert on what the answer *did* rather than on
 * the initial state it happens to agree with.
 */
function statusIs(available: boolean): Promise<void> {
  let read!: () => void;
  const consumed = new Promise<void>((resolve) => (read = resolve));
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => ({
      ok: true,
      json: async () => {
        read();
        return { available };
      },
    })),
  );
  return consumed;
}

/** The drawer's wrapper — the element whose class and width the shell drives. */
function wrapperOf(drawer: HTMLElement): HTMLElement {
  return drawer.parentElement as HTMLElement;
}

beforeEach(() => {
  Object.defineProperty(window, "localStorage", {
    configurable: true,
    value: (() => {
      const store = new Map<string, string>();
      return {
        getItem: (k: string) => store.get(k) ?? null,
        setItem: (k: string, v: string) => void store.set(k, v),
      };
    })(),
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
  Reflect.deleteProperty(window, "localStorage");
});

/** Open the panel, and hand back the drawer node so identity can be compared. */
async function opened() {
  const user = userEvent.setup();
  render(
    <AgentChatLauncher userKey="u">
      <main>page</main>
    </AgentChatLauncher>,
  );
  await user.click(await screen.findByRole("button", { name: "Ask Dr. Octo" }));
  return { user, drawer: await screen.findByTestId("drawer") };
}

describe("when the agent is not deployed", () => {
  it("renders the page and no launcher", async () => {
    const consumed = statusIs(false);
    render(
      <AgentChatLauncher userKey="u">
        <main>page</main>
      </AgentChatLauncher>,
    );
    expect(screen.getByRole("main")).toBeInTheDocument();

    // Wait for the answer to have been read and acted on, so this is the probe
    // saying no rather than the initial state that happens to agree with it — the
    // available case below proves the same wait does surface the button.
    await act(async () => {
      await consumed;
    });
    expect(screen.queryByRole("button", { name: "Ask Dr. Octo" })).toBeNull();
    expect(screen.queryByTestId("drawer")).toBeNull();
  });
});

describe("when he is", () => {
  // Braced: returning the promise would make vitest await a probe that nothing
  // has rendered yet, and the hook would sit there until it timed out.
  beforeEach(() => {
    statusIs(true);
  });

  it("offers the launcher, and mounts nothing of the drawer until it is opened", async () => {
    const consumed = statusIs(true);
    render(
      <AgentChatLauncher userKey="u">
        <main>page</main>
      </AgentChatLauncher>,
    );
    await act(async () => {
      await consumed;
    });
    expect(
      screen.getByRole("button", { name: "Ask Dr. Octo" }),
    ).toBeInTheDocument();
    expect(screen.queryByTestId("drawer")).toBeNull();
  });

  it("keeps the same drawer node when the panel is collapsed", async () => {
    const { user, drawer } = await opened();
    await user.click(screen.getByText("collapse"));

    // Still the very same element, only hidden: a fresh one would mean the fetch
    // streaming an answer had been aborted.
    expect(screen.getByTestId("drawer")).toBe(drawer);
    expect(wrapperOf(drawer).className).toContain("hidden");
    await screen.findByRole("button", { name: "Ask Dr. Octo" });
  });

  it("keeps the same drawer node when the panel is pinned and unpinned", async () => {
    const { user, drawer } = await opened();
    expect(drawer.dataset.docked).toBe("false");
    // classList rather than the string: `max-md:fixed` is a different rule, and
    // docked carries it.
    expect(wrapperOf(drawer).classList.contains("fixed")).toBe(true);

    await user.click(screen.getByText("pin"));
    expect(screen.getByTestId("drawer")).toBe(drawer);
    expect(drawer.dataset.docked).toBe("true");
    expect(wrapperOf(drawer).classList.contains("shrink-0")).toBe(true);
    expect(wrapperOf(drawer).classList.contains("fixed")).toBe(false);

    await user.click(screen.getByText("pin"));
    expect(screen.getByTestId("drawer")).toBe(drawer);
    expect(wrapperOf(drawer).classList.contains("fixed")).toBe(true);
  });

  it("keeps the page a strip of the window when docked, and floats when it cannot", async () => {
    const { user, drawer } = await opened();
    await user.click(screen.getByText("pin"));
    const wrapper = wrapperOf(drawer);

    // Never the last 22.5rem of the window...
    expect(wrapper.classList.contains("md:max-w-[calc(100vw-22.5rem)]")).toBe(
      true,
    );
    // ...and under `md`, back to floating rather than crushing the page. jsdom
    // resolves no media queries, so this asserts the rule is on the element; the
    // widths themselves were checked in a browser.
    expect(wrapper.classList.contains("max-md:fixed")).toBe(true);
    expect(wrapper.classList.contains("max-w-[100vw]")).toBe(true);
  });

  it("remembers the pin", async () => {
    const { user } = await opened();
    await user.click(screen.getByText("pin"));
    expect(window.localStorage.getItem("octo.agent.docked")).toBe("true");
  });

  it("haloes the collapsed button while he is working", async () => {
    const { user, drawer } = await opened();
    await act(async () => {
      screen.getByText("work").click();
    });
    await user.click(screen.getByText("collapse"));

    const button = await screen.findByRole("button", {
      name: "Dr. Octo is working…",
    });
    expect(
      button.parentElement?.querySelector(".agent-halo"),
    ).toBeInTheDocument();
    // And the run it is reporting on is still the same mounted drawer.
    expect(screen.getByTestId("drawer")).toBe(drawer);
  });
});
