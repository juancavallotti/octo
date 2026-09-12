"use client";

import { createContext, useContext, useMemo, type ReactNode } from "react";

/**
 * Editor preferences: choices about how the editor behaves that belong to the person
 * using it, not to the document they have open.
 *
 * The editor reads them and never writes them. Where they are *set* is the host's
 * business and differs by host — Octo Desktop has a Settings window, the platform will
 * read them from the signed-in user's profile — and a preferences UI inside an
 * embeddable editor would be a second place to change something the host already owns.
 * So this is a plain value passed down from the host, with a default for every field.
 *
 * Every default is the conservative answer, because a host that has not been taught
 * about a preference yet passes nothing.
 */
export interface EditorPrefs {
  /**
   * Run flows in the background, by the editor's own decision, to learn what their
   * messages look like — feeding CEL completion without waiting for the user to press ▶.
   *
   * Off unless a host says otherwise. Only flows that do nothing observable outside the
   * process are ever eligible (see run/pure.ts), but "the editor runs your flows" is
   * still a thing to be asked about rather than assumed.
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
