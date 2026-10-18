import { Fragment } from "react";

// An emoji plus its optional variation selector: the glyphs a terminal draws two
// cells wide.
const WIDE = /(\p{Extended_Pictographic}️?)/u;

/**
 * One line of process output drawn on a terminal's grid. A terminal gives an emoji
 * exactly two cells, and output that frames itself (the runtime's ready banner) is
 * laid out on that promise. A browser draws the emoji from a fallback font at its
 * own width instead, so we pin each one to two monospace cells to keep the columns.
 */
export default function TerminalText({ text }: { text: string }) {
  // split with a capture group interleaves the matches at the odd indexes.
  const parts = text.split(WIDE);
  return parts.map((part, i) =>
    i % 2 === 1 ? (
      <span key={i} className="inline-block w-[2ch] text-center">
        {part}
      </span>
    ) : (
      <Fragment key={i}>{part}</Fragment>
    ),
  );
}
