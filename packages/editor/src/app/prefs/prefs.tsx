"use client";

import { createContext, useContext, useMemo, type ReactNode } from "react";

/**
 * Editor preferences: choices about how the editor behaves that belong to the person
 * using it, not to the document they have open.
 *
 * The editor reads them and never writes them: this is a plain value passed in, with a
 * default for every field. Every default is the conservative answer, because a caller
 * that has not been taught about a preference yet passes nothing.
 */
export interface EditorPrefs {
  /**
   * Run flows in the background, by the editor's own decision, to learn what their
   * messages look like — feeding CEL completion without waiting for the user to press ▶.
   *
   * Off unless asked for. Only flows that do nothing observable outside the process are
   * ever eligible (see run/pure.ts).
   */
  autoLearn: boolean;
}

export const DEFAULT_PREFS: EditorPrefs = { autoLearn: false };

const PrefsContext = createContext<EditorPrefs>(DEFAULT_PREFS);

export function EditorPrefsProvider({
  prefs,
  children,
}: {
  prefs?: Partial<EditorPrefs> | null;
  children: ReactNode;
}) {
  // Memoized on the fields rather than on the object, so a host that rebuilds its
  // prefs literal every render does not re-run every consumer.
  const value = useMemo<EditorPrefs>(
    () => ({ ...DEFAULT_PREFS, ...(prefs ?? {}) }),
    [prefs?.autoLearn],
  );
  return <PrefsContext.Provider value={value}>{children}</PrefsContext.Provider>;
}

/** The editor's preferences. Always a complete value — an absent host means defaults. */
export function useEditorPrefs(): EditorPrefs {
  return useContext(PrefsContext);
}
