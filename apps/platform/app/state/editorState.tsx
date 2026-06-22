"use client";

import { ReducerAction, bootstrapProvider } from "@eetr/react-reducer-utils";
import { EditorActionType } from "./actions";
import { EditorState, initialState, reducer } from "./reducer";

/**
 * Wires the editor reducer (reducer.ts) into a context provider. State shape and
 * action handling live in their own modules to keep this file thin; consumers
 * import the provider and the `useEditorState` accessor from here.
 */
const { Provider, useContextAccessors } = bootstrapProvider<
  EditorState,
  ReducerAction<EditorActionType>
>(reducer, initialState);

export { Provider as EditorStateProvider, useContextAccessors as useEditorState };
export { EditorActionType } from "./actions";
export type { EditorState } from "./reducer";
