"use client";

import { Fragment } from "react";
import type { FlowDoc } from "@/app/model/document";
import { isComposite } from "@/app/model/document";
import BlockCard from "./BlockCard";
import CompositeCard from "./CompositeCard";
import DropGap from "./DropGap";

/**
 * Renders a flow's process chain as arrow-connected nodes with an insertion gap
 * before every node and after the last one. This is the recursive unit of the
 * canvas: a composite block reuses it for each of its nested sub-flows, so drops
 * land at a precise index in whichever (possibly nested) flow they hover.
 */
export default function FlowView({
  flow,
  ariaLabel,
  emptyHint,
}: {
  flow: FlowDoc;
  ariaLabel: string;
  emptyHint: string;
}) {
  const { process } = flow;

  return (
    <div
      role="list"
      aria-label={ariaLabel}
      className="flex w-full flex-col items-center"
    >
      {process.length === 0 ? (
        <DropGap flowId={flow.id} index={0} empty hint={emptyHint} />
      ) : (
        <>
          <DropGap flowId={flow.id} index={0} />
          {process.map((block, i) => (
            <Fragment key={block.id}>
              <div role="listitem" className="flex w-full flex-col items-center">
                {isComposite(block.type) ? (
                  <CompositeCard block={block} flowId={flow.id} />
                ) : (
                  <BlockCard block={block} flowId={flow.id} />
                )}
              </div>
              <DropGap
                flowId={flow.id}
                index={i + 1}
                last={i === process.length - 1}
              />
            </Fragment>
          ))}
        </>
      )}
    </div>
  );
}
