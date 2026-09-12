/**
 * Undo/redo as a wrapper around a reducer, not a feature inside one.
 *
 * The editor's reducer is already pure over immutable state, which is the whole
 * precondition: every edit hands back a fresh state object and leaves the previous
 * one intact, so a history is a list of states we already have rather than a log of
 * inverse operations we would have to write and keep correct for thirty-odd actions.
 * Nothing in handlers.ts knows this exists.
 *
 * Snapshots are structurally shared — an edit to one block reuses every other block —
 * so the cost of a past entry is a pointer, not a document.
 */

/** How far back undo reaches. Deep enough to rescue a mistake, not a session log. */
export const DEFAULT_LIMIT = 50;

/** What an action does to the history. */
export type Tracking =
  /** Changes the document: push the previous state onto the past. */
  | "record"
  /** Changes only the view or the selection: move forward, remember nothing. */
  | "skip"
  /** A document arrived from outside, so everything before it is about another file. */
  | "reset";

export interface Historied<S, A> {
  past: S[];
  present: S;
  future: S[];
  /**
   * The action that produced `present`, and the only reason this is not a plain
   * triple: coalescing has to compare the incoming action with the last recorded one,
   * and the reducer is handed nothing else that remembers it.
   */
  last?: A;
}

export function initialHistory<S, A>(present: S): Historied<S, A> {
  return { past: [], present, future: [] };
}

/** Drop the oldest entries once the past outgrows `limit`. */
function capped<S>(past: S[], limit: number): S[] {
  return past.length <= limit ? past : past.slice(past.length - limit);
}

export interface HistoryPolicy<A> {
  track(action: A): Tracking;
  isUndo(action: A): boolean;
  isRedo(action: A): boolean;
  /**
   * A key identifying the edit an action continues, or null when it starts a new one.
   * Two consecutive recorded actions with the same non-null key collapse into a single
   * undo step — which is what makes undo usable in a text field, where every keystroke
   * is its own action.
   */
  coalesceKey?(action: A): string | null;
  limit?: number;
}

export function withHistory<S, A>(
  inner: (state: S, action: A) => S,
  policy: HistoryPolicy<A>,
): (history: Historied<S, A>, action: A) => Historied<S, A> {
  const limit = policy.limit ?? DEFAULT_LIMIT;

  return (history, action) => {
    if (policy.isUndo(action)) {
      const previous = history.past[history.past.length - 1];
      if (previous === undefined) return history;
      return {
        past: history.past.slice(0, -1),
        present: previous,
        future: [history.present, ...history.future],
        // No `last`: the next edit must start its own step rather than coalescing
        // onto the one we just stepped off.
      };
    }

    if (policy.isRedo(action)) {
      const [next, ...rest] = history.future;
      if (next === undefined) return history;
      return { past: capped([...history.past, history.present], limit), present: next, future: rest };
    }

    const present = inner(history.present, action);
    // An action the reducer ignored is not an undo step. Identity is the right test
    // because every handler that changes anything returns a new object.
    if (present === history.present) return history;

    switch (policy.track(action)) {
      case "reset":
        return { past: [], present, future: [] };
      case "skip":
        // `last` is cleared: moving the selection ends whatever was being typed, so
        // the next edit is a step of its own even when it touches the same field.
        return { ...history, present, last: undefined };
      case "record": {
        const key = policy.coalesceKey?.(action) ?? null;
        const sameEdit =
          key !== null && history.last !== undefined && policy.coalesceKey?.(history.last) === key;
        return {
          // Coalescing means keeping the past we already have: the entry it would
          // push is the half-typed intermediate state nobody wants to undo to.
          past: sameEdit ? history.past : capped([...history.past, history.present], limit),
          present,
          // Any edit forks the timeline, so what was redoable no longer is.
          future: [],
          last: action,
        };
      }
    }
  };
}
