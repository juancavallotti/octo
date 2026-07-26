import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { EditorStateProvider } from "../state/editorState";
import DndProvider from "./DndProvider";
import CompositeCard from "./CompositeCard";
import { newBlock } from "../model/document";

function renderBlock(type: string) {
  const block = newBlock(type);
  return render(
    <EditorStateProvider>
      <DndProvider>
        <CompositeCard block={block} flowId="f1" />
      </DndProvider>
    </EditorStateProvider>,
  );
}

describe("composite slot labels", () => {
  it("labels a lone slot that is not the block's body", () => {
    // Split's one slot builds the *response*; the block's real path is the rest
    // of the flow below it. An unlabelled box would read as "this is what Split
    // runs", which is exactly backwards.
    renderBlock("split");
    expect(screen.getByText("Build response")).toBeInTheDocument();
  });

  it("labels the aggregate's response slot too", () => {
    renderBlock("aggregate");
    expect(screen.getByText("Build response")).toBeInTheDocument();
  });

  it("leaves a lone body slot unlabelled", () => {
    // Cache's one slot is what the block memoizes, so the box around it already
    // says what it is and a label would be noise.
    renderBlock("cache-scope");
    expect(screen.queryByText("Body")).not.toBeInTheDocument();
  });

  it("labels the branches of a multi-slot composite", () => {
    renderBlock("if");
    expect(screen.getByText("Then")).toBeInTheDocument();
    expect(screen.getByText("Else")).toBeInTheDocument();
  });

  it("says an optional slot may be left empty", () => {
    renderBlock("split");
    expect(screen.getByText(/Optional/)).toBeInTheDocument();
  });

  it("keeps the node to a label, not the schema's prose", () => {
    // The label already says what the slot is; the full description belongs in
    // the reference, not on a canvas node that has to stay scannable.
    renderBlock("split");
    expect(screen.queryByText(/What to answer the caller with/)).not.toBeInTheDocument();
  });
});
