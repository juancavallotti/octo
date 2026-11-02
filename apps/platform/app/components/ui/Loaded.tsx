import type { ReactNode } from "react";
import Callout from "./Callout";
import type { Loadable } from "./loadable";

/**
 * Renders a {@link Loadable}: the skeleton while it loads, the reason when it
 * failed, and the children with the data once it is ready. The region is marked
 * busy while loading or refreshing, which is what tells a screen reader that the
 * skeleton is a wait rather than content.
 */
export default function Loaded<T>({
  state,
  skeleton,
  children,
}: {
  state: Loadable<T>;
  skeleton: ReactNode;
  children: (data: T, refreshing: boolean) => ReactNode;
}) {
  const busy = state.status === "loading" || (state.status === "ready" && state.refreshing);
  return (
    <div aria-busy={busy} className="contents">
      {state.status === "loading"
        ? skeleton
        : state.status === "failed"
          ? <Callout>{state.error}</Callout>
          : children(state.data, state.refreshing)}
    </div>
  );
}
