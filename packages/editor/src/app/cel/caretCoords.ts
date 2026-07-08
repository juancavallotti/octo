/**
 * Pixel coordinates of the caret within a <textarea>, so the completion menu can be
 * anchored at the caret rather than below a (possibly tall) editor. Uses the
 * well-known mirror-<div> technique: render a hidden div with the textarea's own
 * text styling, place a marker where the caret is, and read the marker's offset.
 *
 * Returns offsets relative to the textarea's top-left (already accounting for the
 * textarea's own scroll), plus the line height — callers typically anchor the menu
 * at `top + height`. In a non-layout environment (jsdom) the offsets are 0, which
 * is harmless: the menu simply renders at the field origin.
 */

// Style properties that affect text wrapping/metrics and must be mirrored.
const MIRRORED_PROPS = [
  "boxSizing",
  "width",
  "paddingTop",
  "paddingRight",
  "paddingBottom",
  "paddingLeft",
  "borderTopWidth",
  "borderRightWidth",
  "borderBottomWidth",
  "borderLeftWidth",
  "fontFamily",
  "fontSize",
  "fontWeight",
  "fontStyle",
  "letterSpacing",
  "lineHeight",
  "textTransform",
  "wordSpacing",
  "tabSize",
  "whiteSpace",
  "wordWrap",
  "wordBreak",
] as const;

export interface CaretPoint {
  left: number;
  top: number;
  height: number;
}

export function caretCoordinates(
  textarea: HTMLTextAreaElement,
  position: number,
): CaretPoint {
  const doc = textarea.ownerDocument;
  const win = doc.defaultView ?? window;
  const computed = win.getComputedStyle(textarea);

  const mirror = doc.createElement("div");
  const style = mirror.style;
  style.position = "absolute";
  style.visibility = "hidden";
  style.whiteSpace = "pre-wrap";
  style.wordWrap = "break-word";
  style.overflow = "hidden";
  for (const prop of MIRRORED_PROPS) {
    style[prop as any] = computed[prop as any];
  }

  mirror.textContent = textarea.value.slice(0, position);
  // A marker at the caret; its offset is the caret position within the mirror.
  const marker = doc.createElement("span");
  marker.textContent = textarea.value.slice(position) || ".";
  mirror.appendChild(marker);

  doc.body.appendChild(mirror);
  const lineHeight =
    parseFloat(computed.lineHeight) || parseFloat(computed.fontSize) * 1.2 || 16;
  const point: CaretPoint = {
    left: marker.offsetLeft - textarea.scrollLeft,
    top: marker.offsetTop - textarea.scrollTop,
    height: lineHeight,
  };
  doc.body.removeChild(mirror);
  return point;
}
