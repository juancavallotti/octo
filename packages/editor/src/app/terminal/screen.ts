/**
 * One terminal screen: an xterm instance drawn into an element of its own, which the
 * Terminal tab moves into its viewport while the session is showing and out again
 * when it is not. Moving the element rather than re-creating the terminal is what
 * keeps a session's scrollback across tab switches.
 *
 * xterm is imported when the first screen is made, not at module scope: it reaches for
 * `window` as it loads, and this module is part of a client tree that is still
 * rendered on the server.
 */

import type { ITheme, Terminal } from "@xterm/xterm";
import type { FitAddon } from "@xterm/addon-fit";

export interface TerminalScreen {
  /** Draw into `viewport`, replacing whatever it showed. */
  mount(viewport: HTMLElement): void;
  unmount(): void;
  /** Resize the grid to the viewport, reporting the new size through `onResize`. */
  fit(): void;
  focus(): void;
  /** Whether the keyboard is in this screen right now. */
  focused(): boolean;
  clear(): void;
  write(chunk: string): void;
  onInput(listener: (data: string) => void): () => void;
  onResize(listener: (size: { cols: number; rows: number }) => void): () => void;
  dispose(): void;
}

// The console's own surfaces (zinc-50 and zinc-900), so the terminal reads as part of
// the panel rather than a box set into it. The light palette darkens the ANSI colours
// that xterm's defaults pick for a dark background, where yellow on white is unreadable.
const LIGHT: ITheme = {
  background: "#fafafa",
  foreground: "#27272a",
  cursor: "#27272a",
  cursorAccent: "#fafafa",
  selectionBackground: "#bae6fd",
  black: "#000000",
  red: "#cd3131",
  green: "#107c10",
  yellow: "#949800",
  blue: "#0451a5",
  magenta: "#bc05bc",
  cyan: "#0598bc",
  white: "#555555",
  brightBlack: "#666666",
  brightRed: "#cd3131",
  brightGreen: "#14ce14",
  brightYellow: "#b5ba00",
  brightBlue: "#0451a5",
  brightMagenta: "#bc05bc",
  brightCyan: "#0598bc",
  brightWhite: "#a5a5a5",
};

const DARK: ITheme = {
  background: "#18181b",
  foreground: "#e4e4e7",
  cursor: "#e4e4e7",
  cursorAccent: "#18181b",
  selectionBackground: "#3f3f46",
};

const DARK_QUERY = "(prefers-color-scheme: dark)";

function theme(): ITheme {
  return window.matchMedia?.(DARK_QUERY).matches ? DARK : LIGHT;
}

/** Whether this keystroke is one of the editor's terminal shortcuts, Ctrl+` with or without Shift. */
export function isTerminalShortcut(e: KeyboardEvent): boolean {
  return e.ctrlKey && !e.metaKey && !e.altKey && e.code === "Backquote";
}

export async function createScreen(): Promise<TerminalScreen> {
  const [{ Terminal }, { FitAddon }] = await Promise.all([
    import("@xterm/xterm"),
    import("@xterm/addon-fit"),
  ]);

  const element = document.createElement("div");
  element.style.width = "100%";
  element.style.height = "100%";

  const term: Terminal = new Terminal({
    // System monospace rather than the app's web font: xterm measures its cell once,
    // when it opens, and a font still loading then leaves the grid mis-sized.
    fontFamily: '"SF Mono", Menlo, Monaco, Consolas, "Courier New", monospace',
    fontSize: 12,
    cursorBlink: true,
    scrollback: 5000,
    allowProposedApi: false,
    macOptionIsMeta: true,
    theme: theme(),
  });
  const fitAddon: FitAddon = new FitAddon();
  term.loadAddon(fitAddon);

  // The editor's shortcuts must reach the editor from inside the terminal too, or
  // the key that shows the terminal could never hide it again.
  term.attachCustomKeyEventHandler((e) => !isTerminalShortcut(e));

  const media = window.matchMedia?.(DARK_QUERY);
  const onScheme = () => {
    term.options.theme = theme();
  };
  media?.addEventListener("change", onScheme);

  let opened = false;
  const event = <T>(subscribe: (listener: (value: T) => void) => { dispose(): void }) =>
    (listener: (value: T) => void) => {
      const sub = subscribe(listener);
      return () => sub.dispose();
    };

  return {
    mount(viewport) {
      viewport.replaceChildren(element);
      // Opened on first mount rather than at creation: xterm measures its cell from
      // the element it opens into, which has no size until it is in the page.
      if (!opened) {
        term.open(element);
        opened = true;
      }
    },
    unmount() {
      element.remove();
    },
    fit() {
      // A collapsed or hidden viewport measures as zero, which would shrink the
      // shell's grid to nothing and reflow everything it printed.
      if (!element.isConnected || element.clientWidth === 0 || element.clientHeight === 0) return;
      fitAddon.fit();
    },
    focus: () => term.focus(),
    focused: () => element.contains(document.activeElement),
    clear: () => term.clear(),
    write: (chunk) => term.write(chunk),
    onInput: event((l) => term.onData(l)),
    onResize: event((l) => term.onResize(l)),
    dispose() {
      media?.removeEventListener("change", onScheme);
      term.dispose();
      element.remove();
    },
  };
}
