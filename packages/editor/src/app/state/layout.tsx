"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

/**
 * Which of the editor's surrounding panels are showing: the palette on the left and
 * the settings panel on the right. The bottom console is not here — it already has
 * an owner (ConsoleProvider), because a run has to be able to open it.
 *
 * A workspace-wide preference rather than a per-document one, like the console's
 * height: someone who works on a laptop hides the palette to get the canvas back,
 * and means it for every file they open, not for the one that happened to be
 * onscreen when they said so.
 *
 * Read from storage in an effect rather than in the initial state, so the server's
 * HTML and the client's first pass agree; a remembered layout appears a tick later
 * instead of tripping hydration.
 */

const KEY = "octo.layout";

interface Stored {
  sidebar: boolean;
  settings: boolean;
}

interface LayoutValue extends Stored {
  toggleSidebar(): void;
  toggleSettings(): void;
}

const LayoutContext = createContext<LayoutValue | null>(null);

/** Both panels showing — what a first-time editor looks like. */
const DEFAULT: Stored = { sidebar: true, settings: true };

function read(): Stored | null {
  // localStorage can be absent (a test env) or throw on access (a sandboxed frame,
  // storage disabled): a remembered layout is a nicety, never worth taking the
  // editor down over.
  try {
    const raw = window.localStorage?.getItem(KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as Partial<Stored>;
    return {
      sidebar: parsed.sidebar ?? DEFAULT.sidebar,
      settings: parsed.settings ?? DEFAULT.settings,
    };
  } catch {
    return null;
  }
}

export function LayoutProvider({ children }: { children: ReactNode }) {
  const [layout, setLayout] = useState<Stored>(DEFAULT);

  useEffect(() => {
    const stored = read();
    if (stored) setLayout(stored);
  }, []);

  const write = useCallback((next: Stored) => {
    setLayout(next);
    try {
      window.localStorage?.setItem(KEY, JSON.stringify(next));
    } catch {
      // Not remembering the choice is survivable; refusing to make it is not.
    }
  }, []);

  const value = useMemo<LayoutValue>(
    () => ({
      ...layout,
      toggleSidebar: () => write({ ...layout, sidebar: !layout.sidebar }),
      toggleSettings: () => write({ ...layout, settings: !layout.settings }),
    }),
    [layout, write],
  );

  return (
    <LayoutContext.Provider value={value}>{children}</LayoutContext.Provider>
  );
}

/**
 * The panel layout. Always mounted by EditorRoot, so a missing provider is a wiring
 * bug rather than a supported mode — but the controls are rendered by app-owned
 * headers, and a host that composes one outside the editor should not crash the
 * page over a chrome toggle. Hence null rather than a throw, and the toggles hide
 * themselves.
 */
export function useLayout(): LayoutValue | null {
  return useContext(LayoutContext);
}
