"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { createScreen, type TerminalScreen } from "../terminal/screen";

/**
 * The editor's terminal capability: shells the host can start for the person using
 * the editor. Only a host with a machine of its own to run them on can supply one — a
 * desktop shell can, a web page cannot — so it is optional, and when it is absent the
 * Terminal tab is not there at all.
 *
 * The provider owns the sessions rather than the tab, because a terminal outlives
 * looking at it: switching tabs or collapsing the console must not end a shell or lose
 * what it printed.
 */

/** One running shell, as the host started it. */
export interface TerminalSession {
  id: string;
  /** What the session list calls it: the shell's name, "zsh" say. */
  title: string;
  /** Keystrokes, already encoded the way a terminal sends them. */
  write(data: string): void;
  resize(cols: number, rows: number): void;
  /** End the shell. Its exit is still reported through `onExit`. */
  kill(): void;
  /** Output, in the order the shell wrote it. Returns the unsubscribe. */
  onData(listener: (chunk: string) => void): () => void;
  onExit(listener: (code: number) => void): () => void;
}

/** What the editor needs from a host to give it terminals. */
export interface TerminalTransport {
  /** Start a shell sized to the grid it will be drawn on. */
  open(size: { cols: number; rows: number }): Promise<TerminalSession>;
}

/** A session as the session list shows it. */
export interface TerminalEntry {
  id: string;
  title: string;
  /** The shell's exit code once it has ended, else null. */
  exitCode: number | null;
}

export interface TerminalController {
  sessions: TerminalEntry[];
  active: string | null;
  /** Why the last shell asked for did not start, until the next one is asked for. */
  error: string | null;
  select(id: string): void;
  /** Start a new shell and make it the active one. A failure lands in `error`. */
  open(): Promise<void>;
  /** Start a shell unless one is open or already starting: what showing the tab does. */
  ensure(): Promise<void>;
  /** Kill a session (if it is still running) and drop it from the list. */
  close(id: string): void;
  /** Draw the active session into `viewport`; returns the detach. */
  attach(viewport: HTMLElement): () => void;
  /** Refit the active session to its viewport, after the panel changed size. */
  fit(): void;
  focus(): void;
  /** Whether the keyboard is in the active session. */
  focused(): boolean;
  clear(): void;
}

// The grid a shell starts on before its screen has been measured; the first fit
// resizes it, as a terminal emulator's first frame does.
const INITIAL_SIZE = { cols: 80, rows: 24 };

interface Live {
  session: TerminalSession;
  screen: TerminalScreen;
  unsubscribe: () => void;
}

const TerminalContext = createContext<TerminalController | null>(null);

export function TerminalProvider({
  value,
  children,
}: {
  value: TerminalTransport | null;
  children: ReactNode;
}) {
  // The same element whether or not there is a transport: a host that only learns it
  // has one after hydration must not remount the whole editor to say so.
  const controller = useSessions(value);
  return (
    <TerminalContext.Provider value={value ? controller : null}>{children}</TerminalContext.Provider>
  );
}

function useSessions(transport: TerminalTransport | null): TerminalController {
  const [sessions, setSessions] = useState<TerminalEntry[]>([]);
  const [active, setActive] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const live = useRef(new Map<string, Live>());
  const activeRef = useRef<string | null>(null);
  activeRef.current = active;
  const sessionsRef = useRef(sessions);
  sessionsRef.current = sessions;

  const patch = useCallback((id: string, change: Partial<TerminalEntry>) => {
    setSessions((list) => list.map((s) => (s.id === id ? { ...s, ...change } : s)));
  }, []);

  // Shells asked for but not yet started, so showing the tab while a New Terminal is on
  // its way does not start a second one.
  const pending = useRef(0);

  // Set once the editor has gone, so a shell that finishes starting afterwards is ended
  // rather than kept by a provider nothing renders.
  const gone = useRef(false);

  const start = useCallback(async () => {
    if (!transport) return;
    // The screen first: a shell starts talking at once, and its prompt must have
    // somewhere to land rather than arrive while xterm is still loading.
    const screen = await createScreen();
    let session: TerminalSession;
    try {
      session = await transport.open(INITIAL_SIZE);
    } catch (err) {
      screen.dispose();
      throw err;
    }
    if (gone.current) {
      session.kill();
      screen.dispose();
      return;
    }
    const offs = [
      screen.onInput((data) => session.write(data)),
      screen.onResize(({ cols, rows }) => session.resize(cols, rows)),
      session.onData((chunk) => screen.write(chunk)),
      session.onExit((code) => {
        screen.write(`\r\n\x1b[2m[process exited with code ${code}]\x1b[0m\r\n`);
        patch(session.id, { exitCode: code });
      }),
    ];
    live.current.set(session.id, {
      session,
      screen,
      unsubscribe: () => offs.forEach((off) => off()),
    });
    setSessions((list) => [...list, { id: session.id, title: session.title, exitCode: null }]);
    setActive(session.id);
  }, [transport, patch]);

  const open = useCallback(async () => {
    pending.current += 1;
    setError(null);
    try {
      await start();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      pending.current -= 1;
    }
  }, [start]);

  const ensure = useCallback(async () => {
    if (sessionsRef.current.length > 0 || pending.current > 0) return;
    await open();
  }, [open]);

  const close = useCallback((id: string) => {
    const entry = live.current.get(id);
    if (!entry) return;
    live.current.delete(id);
    entry.unsubscribe();
    entry.session.kill();
    entry.screen.dispose();
    const list = sessionsRef.current;
    const at = list.findIndex((s) => s.id === id);
    const rest = list.filter((s) => s.id !== id);
    setSessions(rest);
    // The neighbour that slides into the closed row's place, as a tab strip does.
    if (activeRef.current === id) setActive(rest[Math.min(at, rest.length - 1)]?.id ?? null);
  }, []);

  const attach = useCallback(
    (viewport: HTMLElement) => {
      const entry = active ? live.current.get(active) : undefined;
      if (!entry) return () => {};
      entry.screen.mount(viewport);
      entry.screen.fit();
      entry.screen.focus();
      return () => entry.screen.unmount();
    },
    [active],
  );

  const current = () => (activeRef.current ? live.current.get(activeRef.current) : undefined);
  const fit = useCallback(() => current()?.screen.fit(), []);
  const focus = useCallback(() => current()?.screen.focus(), []);
  const focused = useCallback(() => current()?.screen.focused() ?? false, []);
  const clear = useCallback(() => current()?.screen.clear(), []);

  // Leaving the editor ends its shells: nothing else holds a way to reach them.
  useEffect(() => {
    const sessions = live.current;
    // Cleared on (re)mount: a development remount runs this cleanup once in between.
    gone.current = false;
    return () => {
      gone.current = true;
      for (const entry of sessions.values()) {
        entry.unsubscribe();
        entry.session.kill();
        entry.screen.dispose();
      }
      sessions.clear();
      // The list goes with the shells: a remount that keeps state (Fast Refresh, a
      // development double mount) must not show rows for sessions that are gone.
      setSessions([]);
      setActive(null);
    };
  }, []);

  return useMemo<TerminalController>(
    () => ({
      sessions,
      active,
      error,
      select: setActive,
      open,
      ensure,
      close,
      attach,
      fit,
      focus,
      focused,
      clear,
    }),
    [sessions, active, error, open, ensure, close, attach, fit, focus, focused, clear],
  );
}

/** The editor's terminals, or null when the host supplied none. */
export function useTerminal(): TerminalController | null {
  return useContext(TerminalContext);
}
