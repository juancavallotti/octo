import { describe, expect, it } from "vitest";
import { WHOLE_TEXT_SCOPE, handlebarsScopeAt } from "./useCelCompletion";

describe("WHOLE_TEXT_SCOPE", () => {
  it("covers the entire text", () => {
    expect(WHOLE_TEXT_SCOPE("body.x", 3)).toEqual({ start: 0, end: 6 });
  });
});

describe("handlebarsScopeAt", () => {
  it("returns the inner range when the caret is inside {{ }}", () => {
    const text = "Hi {{ body }}!";
    // caret right after "body"
    const caret = text.indexOf("body") + 4;
    expect(handlebarsScopeAt(text, caret)).toEqual({
      start: text.indexOf("{{") + 2,
      end: text.indexOf("}}"),
    });
  });

  it("returns null when the caret is outside any braces", () => {
    const text = "Hi {{ body }} there";
    expect(handlebarsScopeAt(text, text.length)).toBeNull();
    expect(handlebarsScopeAt(text, 1)).toBeNull();
  });

  it("extends an unclosed {{ to end of text", () => {
    const text = "Hi {{ bod";
    expect(handlebarsScopeAt(text, text.length)).toEqual({
      start: text.indexOf("{{") + 2,
      end: text.length,
    });
  });

  it("scopes to the correct block among several", () => {
    const text = "{{ a }} mid {{ body }}";
    const caret = text.lastIndexOf("body") + 4;
    expect(handlebarsScopeAt(text, caret)).toEqual({
      start: text.lastIndexOf("{{") + 2,
      end: text.lastIndexOf("}}"),
    });
  });
});
