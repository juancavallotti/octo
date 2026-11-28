"use client";

import { createContext, useContext, useEffect, useRef, type ReactNode } from "react";
import { ReducerAction, bootstrapProvider } from "@eetr/react-reducer-utils";
import { EditorActionType } from "./actions";
import { EditorState, initialState, reducer } from "./reducer";
import { initialHistory, withHistory } from "./history";
import { EDITOR_HISTORY } from "./historyPolicy";

/**
 * Wires the editor reducer (reducer.ts) into a context provider. State shape and
 * action handling live in their own modules to keep this file thin; consumers
 * import the provider and the `useEditorState` accessor from here.
 *
 * What the provider actually holds is the reducer wrapped in undo history
 * (history.ts), so the state in context is a past/present/future triple. That is an
 * implementation detail of this file: `useEditorState` projects the present back out,
 * and every consumer sees the same `{ state, dispatch }` it always did.
 */
type Action = ReducerAction<EditorActionType>;

const { Provider, useContextAccessors } = bootstrapProvider<
  ReturnType<typeof initialHistory<EditorState, Action>>,
  Action
>(withHistory(reducer, EDITOR_HISTORY), initialHistory(initialState));

function useEditorState(): {
  state: EditorState;
  dispatch: (action: Action) => void;
  canUndo: boolean;
  canRedo: boolean;
} {
  const { state, dispatch } = useContextAccessors();
  return {
    state: state.present,
    dispatch,
    canUndo: state.past.length > 0,
    canRedo: state.future.length > 0,
  };
}

/**
 * The present state as of the last commit, held in a ref so that work which outlives
 * the component that started it (a request still in flight when its form unmounts)
 * can read the project as it is when the work finishes rather than as it was.
 */
const LatestStateContext = createContext<{ current: EditorState } | null>(null);

function LatestState({ children }: { children: ReactNode }) {
  const { state } = useContextAccessors();
  const ref = useRef(state.present);
  useEffect(() => {
    ref.current = state.present;
  });
  return <LatestStateContext.Provider value={ref}>{children}</LatestStateContext.Provider>;
}

function EditorStateProvider({ children }: { children: ReactNode }) {
  return (
    <Provider>
      <LatestState>{children}</LatestState>
    </Provider>
  );
}

/** Reads the present state when called, not when the calling component rendered. */
function useLatestEditorState(): () => EditorState {
  const ref = useContext(LatestStateContext);
  if (!ref) throw new Error("useLatestEditorState must be used inside EditorStateProvider");
  return () => ref.current;
}

export { EditorStateProvider, useEditorState, useLatestEditorState };
export { EditorActionType } from "./actions";
export type { EditorState } from "./reducer";
