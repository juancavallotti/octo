import { describe, expect, it } from "vitest";
import {
  ZOOM_MAX,
  ZOOM_MIN,
  clampZoom,
  contentPointAt,
  fitZoom,
  nextStep,
  prevStep,
  scrollToHold,
} from "./zoom";

describe("clampZoom", () => {
  it("holds the factor inside the range", () => {
    expect(clampZoom(5)).toBe(ZOOM_MAX);
    expect(clampZoom(0.01)).toBe(ZOOM_MIN);
    expect(clampZoom(1)).toBe(1);
  });

  it("treats a value that is not a number as no zoom at all", () => {
    expect(clampZoom(Number.NaN)).toBe(1);
    expect(clampZoom(Number.POSITIVE_INFINITY)).toBe(1);
  });
});

describe("the zoom steps", () => {
  it("moves one stop at a time", () => {
    expect(nextStep(1)).toBe(1.25);
    expect(prevStep(1)).toBe(0.75);
  });

  it("stops at the ends rather than running past them", () => {
    expect(nextStep(ZOOM_MAX)).toBe(ZOOM_MAX);
    expect(prevStep(ZOOM_MIN)).toBe(ZOOM_MIN);
  });

  it("comes back to where it started", () => {
    // Stops rather than a multiplier, so three out and three back is the
    // identity — which a repeated ×0.8 / ×1.25 is not.
    let zoom = 1;
    for (let i = 0; i < 3; i++) zoom = prevStep(zoom);
    for (let i = 0; i < 3; i++) zoom = nextStep(zoom);
    expect(zoom).toBe(1);
  });

  it("lands on a stop from a factor between two of them", () => {
    expect(nextStep(0.9)).toBe(1);
    expect(prevStep(0.9)).toBe(0.75);
  });
});

describe("holding a point under the cursor", () => {
  it("reads the content point the pointer is over", () => {
    // 100px scrolled, pointer 50px into the view, drawn at half size: the
    // pointer is over content x = 300.
    expect(contentPointAt({ scroll: 100, pointer: 50 }, 0.5)).toBe(300);
  });

  it("puts that point back under the pointer at the new zoom", () => {
    const anchor = { scroll: 100, pointer: 50 };
    const point = contentPointAt(anchor, 0.5);
    const scroll = scrollToHold(point, anchor.pointer, 1);
    expect(scroll).toBe(250);
    // Which is the definition being pinned: reading it back gives the same point.
    expect(contentPointAt({ scroll, pointer: anchor.pointer }, 1)).toBe(point);
  });

  it("does not ask a scroller to move past its own start", () => {
    expect(scrollToHold(10, 500, 1)).toBe(0);
  });
});

describe("fitZoom", () => {
  it("shrinks content that has outgrown the window", () => {
    expect(fitZoom(1000, 532)).toBe(0.5);
  });

  it("never blows a small flow up to fill a wide screen", () => {
    expect(fitZoom(200, 2000)).toBe(1);
  });

  it("stops at the far end rather than vanishing", () => {
    expect(fitZoom(100_000, 500)).toBe(ZOOM_MIN);
  });

  it("says 1 when there is nothing to measure", () => {
    expect(fitZoom(0, 800)).toBe(1);
    expect(fitZoom(800, 0)).toBe(1);
  });
});
