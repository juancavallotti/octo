import { useSyncExternalStore } from "react";
import type { TerminalSession, TerminalTransport } from "@octo/editor";
import { desktopBridge, type TerminalBridge } from "@/app/desktop";

/**
 * The editor's terminals, run by the desktop shell. The shell sends every session's
 * output down one channel tagged with its id, so this sorts it to the session it
 * belongs to.
 */
function transportOver(bridge: TerminalBridge): TerminalTransport {
  const data = new Map<string, Set<(chunk: string) => void>>();
  const exits = new Map<string, Set<(code: number) => void>>();
  bridge.onData((id, chunk) => data.get(id)?.forEach((l) => l(chunk)));
  bridge.onExit((id, code) => {
    exits.get(id)?.forEach((l) => l(code));
    data.delete(id);
    exits.delete(id);
  });

  const listen = <T>(map: Map<string, Set<T>>, id: string, listener: T) => {
    const set = map.get(id) ?? new Set<T>();
    map.set(id, set);
    set.add(listener);
    return () => {
      set.delete(listener);
    };
  };

  return {
    async open({ cols, rows }): Promise<TerminalSession> {
      const { id, title } = await bridge.open(cols, rows);
      return {
        id,
        title,
        write: (chunk) => bridge.write(id, chunk),
        resize: (c, r) => bridge.resize(id, c, r),
        kill: () => bridge.kill(id),
        onData: (listener) => listen(data, id, listener),
        onExit: (listener) => listen(exits, id, listener),
      };
    },
  };
}

let transport: TerminalTransport | null | undefined;

/** One transport per page, so there is one subscription to the shell's channels. */
function snapshot(): TerminalTransport | null {
  if (transport === undefined) {
    const bridge = desktopBridge()?.terminal;
    // Checked at runtime: an older shell reaches here typed as if it had a terminal.
    transport = typeof bridge?.open === "function" ? transportOver(bridge) : null;
  }
  return transport;
}

const never = () => () => {};

/**
 * The desktop shell's terminals, or null in a plain browser. Null on the server and
 * through hydration as well, so the tree the editor wraps in its terminal provider is
 * the same one on both sides until the page is live.
 */
export function useDesktopTerminal(): TerminalTransport | null {
  return useSyncExternalStore(never, snapshot, () => null);
}
