#!/usr/bin/env python3
"""Build an .excalidraw scene from a compact description.

The scenes here are starting points, not finished art: open one in Excalidraw,
move things until they read well, then export a PNG into apps/docs/public/diagrams.
Layout and labels come from here so they stay accurate; the hand goes on top.
"""
import json, random, sys

FONT_HAND = 8      # Comic Shanns, the hand-drawn monospace the other diagrams use
BLACK = "#1e1e1e"

def _rand():
    return random.randint(1, 2**31 - 1)

def _base(kind, x, y, w, h, **over):
    el = {
        "id": over.pop("id", f"e{_rand()}"),
        "type": kind, "x": x, "y": y, "width": w, "height": h, "angle": 0,
        "strokeColor": BLACK, "backgroundColor": "transparent", "fillStyle": "solid",
        "strokeWidth": 2, "strokeStyle": "solid", "roughness": 1, "opacity": 100,
        "groupIds": [], "frameId": None, "roundness": None, "seed": _rand(),
        "version": 1, "versionNonce": _rand(), "isDeleted": False,
        "boundElements": [], "updated": 1, "link": None, "locked": False,
    }
    el.update(over)
    return el

def box(id, x, y, w, h, label, size=16):
    """A rounded rectangle with its label bound inside it."""
    text_id = f"{id}-t"
    rect = _base("rectangle", x, y, w, h, id=id, roundness={"type": 3},
                 boundElements=[{"id": text_id, "type": "text"}])
    lines = label.split("\n")
    text = _base("text", x + 8, y + h / 2 - (size * 1.25 * len(lines)) / 2,
                 w - 16, size * 1.25 * len(lines), id=text_id,
                 fontSize=size, fontFamily=FONT_HAND, text=label, originalText=label,
                 textAlign="center", verticalAlign="middle", containerId=id,
                 lineHeight=1.25, autoResize=True)
    return [rect, text]

def cylinder(id, x, y, w, h, label, size=16):
    """Excalidraw has no cylinder; an ellipse reads as a store well enough."""
    text_id = f"{id}-t"
    el = _base("ellipse", x, y, w, h, id=id,
               boundElements=[{"id": text_id, "type": "text"}])
    text = _base("text", x + 8, y + h / 2 - size * 0.6, w - 16, size * 1.25, id=text_id,
                 fontSize=size, fontFamily=FONT_HAND, text=label, originalText=label,
                 textAlign="center", verticalAlign="middle", containerId=id,
                 lineHeight=1.25, autoResize=True)
    return [el, text]

def arrow(x1, y1, x2, y2, label=None, size=14, frm=None, to=None, gap=6):
    """A straight arrow, with an optional label floating beside its midpoint.

    Passing `frm`/`to` binds the ends to those element ids, so dragging a box in
    Excalidraw drags its arrows with it. Binding is one-sided in the file format:
    the arrow names the shape, and the shape has to name the arrow back, which
    `write` does once every arrow is known.
    """
    a = _base("arrow", x1, y1, x2 - x1, y2 - y1,
              points=[[0, 0], [x2 - x1, y2 - y1]], lastCommittedPoint=None,
              startBinding=({"elementId": frm, "focus": 0, "gap": gap} if frm else None),
              endBinding=({"elementId": to, "focus": 0, "gap": gap} if to else None),
              startArrowhead=None, endArrowhead="arrow", roundness={"type": 2})
    out = [a]
    if label:
        # Offset to the side of the line rather than centred on it: a label that
        # sits on its own arrow is the first thing that has to be dragged after
        # opening one of these.
        w = len(label) * size * 0.62
        mx, my = (x1 + x2) / 2, (y1 + y2) / 2
        if abs(y2 - y1) > abs(x2 - x1):
            lx, ly = mx + 14, my - size * 0.6      # mostly vertical: put it beside
        else:
            lx, ly = mx - w / 2, my - size - 10    # mostly horizontal: put it above
        out.append(_base("text", lx, ly, w, size * 1.25, fontSize=size,
                         fontFamily=FONT_HAND, text=label, originalText=label,
                         textAlign="left" if abs(y2 - y1) > abs(x2 - x1) else "center",
                         verticalAlign="top", containerId=None, lineHeight=1.25,
                         autoResize=True))
    return out

def scene(elements):
    return {
        "type": "excalidraw", "version": 2, "source": "octo/apps/docs/diagrams",
        "elements": elements,
        "appState": {"gridSize": 20, "viewBackgroundColor": "#ffffff"},
        "files": {},
    }

def _bind_back(elements):
    """Add each bound arrow to its shape's boundElements, which Excalidraw needs
    on both sides before it will move an arrow with the shape it points at."""
    by_id = {el["id"]: el for el in elements}
    for el in elements:
        if el["type"] != "arrow":
            continue
        for side in ("startBinding", "endBinding"):
            binding = el.get(side)
            if not binding:
                continue
            shape = by_id.get(binding["elementId"])
            if shape is not None:
                shape["boundElements"].append({"id": el["id"], "type": "arrow"})
    return elements

def write(path, elements):
    elements = _bind_back(elements)
    with open(path, "w", encoding="utf-8") as f:
        json.dump(scene(elements), f, indent=2)
    print("wrote", path, f"({len(elements)} elements)")
