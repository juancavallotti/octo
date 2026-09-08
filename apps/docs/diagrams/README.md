# Diagram sources

Excalidraw scenes for the docs. The `.excalidraw` files here are the source; the
PNGs the docs actually reference live in `../public/diagrams/`.

The scenes are generated, then drawn on. `build.py` holds the shared helpers and
each diagram is one small script beside it, so the layout and the labels come from
something that can be re-run and reviewed rather than from memory. What comes out
is a starting point: open it in Excalidraw, move things until they read well, and
export the PNG. The wobble is the point, and it is the half a generator cannot do.

```bash
python3 platform-architecture.py     # scene   -> platform-architecture.excalidraw
node export.mjs platform-architecture  # PNG   -> ../public/diagrams/platform-architecture.png
```

`export.mjs` renders through Excalidraw's own `exportToCanvas`, so what ships is
what the editor draws, cropped to the drawing's bounds. It serves this directory
while it runs, because the page has to fetch the scene and a `file://` page
cannot.

One wrinkle it handles: the published browser bundle stops at 0.17.6 and knows
font ids 1, 2 and 3, while today's excalidraw.com writes 5 to 8. An id the bundle
does not know paints as a serif, so the exporter maps the hand-drawn ones onto
Virgil and the monospace one onto Cascadia. Only the export is rewritten, never
the source.

To edit a scene: open it at excalidraw.com, change it, share a link, and the
scene comes back out of the browser's own storage. Arrows are bound to the shapes
they connect, so dragging a box drags its arrows with it.
