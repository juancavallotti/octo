import type { ReactNode } from "react";
import Callout from "./Callout";
import type { Loadable } from "./loadable";

/**
 * Renders a {@link Loadable}: the skeleton while it loads, the reason when it
 * failed, and the children with the data once it is ready. The wait is announced
 * as a status and a failure as an alert, since the skeleton itself is hidden from
 * assistive technology; the region is also marked busy while loading or
 * refreshing.
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
      {state.status === "loading" ? (
        <>
          {/* The skeleton is hidden from assistive technology; this says the wait. */}
          <span role="status" className="sr-only">
            Loading…
          </span>
          {skeleton}
        </>
      ) : state.status === "failed" ? (
        <div role="alert">
          <Callout>{state.error}</Callout>
        </div>
      ) : (
        children(state.data, state.refreshing)
      )}
    </div>
  );
}
