"use client";

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

export { Provider as EditorStateProvider, useEditorState };
export { EditorActionType } from "./actions";
export type { EditorState } from "./reducer";
