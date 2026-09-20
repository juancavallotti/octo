/**
 * The composer's two contracts: the keyboard conventions, which were never
 * covered, and the box growing with the draft, which is why this file exists.
 */

import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

import Composer from "./Composer";

const LINE = 20;
const PADDING = 6;

/**
 * jsdom lays nothing out, so the textarea is given the two measurements the hook
 * reads. scrollHeight follows the value's line count, and a held height still
 * wins — the browser's own rule, and the one the shrink case turns on.
 */
function measurable(content = () => LINE + PADDING) {
  const proto = window.HTMLTextAreaElement.prototype;
  const original = Object.getOwnPropertyDescriptor(proto, "scrollHeight");
  Object.defineProperty(proto, "scrollHeight", {
    configurable: true,
    get(this: HTMLTextAreaElement) {
      const held = parseFloat(this.style.height);
      return Number.isFinite(held) ? Math.max(content(), held) : content();
    },
  });
  return () => {
    // Deleted rather than left in place when there was nothing to restore: jsdom
    // defines no scrollHeight on the prototype, so `original` is undefined and a
    // conditional restore would leave this stub installed for every test that ran
    // afterwards.
    if (original) {
      Object.defineProperty(proto, "scrollHeight", original);
    } else {
      Reflect.deleteProperty(proto, "scrollHeight");
    }
  };
}

/**
 * Mounts empty and hands back a `type` that re-renders with a new draft.
 *
 * Empty first because the element does not exist to be given its measurements
 * until after the first render, and the hook has already run by then — so the
 * mount is never the render under test.
 */
function draw() {
  const onSubmit = vi.fn();
  const onDraft = vi.fn();
  const render1 = (draft: string) => (
    <Composer
      draft={draft}
      onDraft={onDraft}
      onSubmit={onSubmit}
      busy={false}
      onStop={vi.fn()}
    />
  );

  const view = render(render1(""));
  const box = screen.getByLabelText("Message") as HTMLTextAreaElement;
  box.style.lineHeight = `${LINE}px`;
  box.style.paddingTop = `${PADDING / 2}px`;
  box.style.paddingBottom = `${PADDING / 2}px`;

  return {
    view,
    box,
    onSubmit,
    onDraft,
    type: (draft: string) => view.rerender(render1(draft)),
    /** The height the hook settled on, in pixels. */
    height: () => parseFloat(box.style.height),
  };
}

describe("Composer", () => {
  it("carries no max-height class: the cap is measured, not styled", () => {
    const { box } = draw();

    // Two answers to one question is how the old `max-h-32` came to be dead code
    // — it clamped a height that never changed.
    expect(box.className).not.toMatch(/max-h-/);
  });

  it("grows with the draft and stops at four lines", () => {
    let lines = 1;
    const restore = measurable(() => LINE * lines + PADDING);
    try {
      const { box, type, height } = draw();

      lines = 3;
      type("a\nb\nc");
      expect(height()).toBe(LINE * 3 + PADDING);

      lines = 12;
      type("a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl");
      expect(height()).toBe(LINE * 4 + PADDING);
      expect(box.style.overflowY).toBe("auto");
    } finally {
      restore();
    }
  });

  it("shrinks back to one line once the draft is sent", () => {
    let lines = 1;
    const restore = measurable(() => LINE * lines + PADDING);
    try {
      const { box, type, height } = draw();

      lines = 5;
      type("five\nlines\nof\na\ndraft");
      expect(height()).toBe(LINE * 4 + PADDING);

      // Submitting clears the draft in the drawer above; the box has to follow it
      // back down rather than keep the space it grew into. Without releasing the
      // height before measuring, scrollHeight still reports the four lines it is
      // holding and the box never comes back.
      lines = 1;
      type("");

      expect(height()).toBe(LINE + PADDING);
      expect(box.style.overflowY).toBe("hidden");
    } finally {
      restore();
    }
  });

  it("sends on Enter", () => {
    const { box, onSubmit, type } = draw();
    type("ready");

    fireEvent.keyDown(box, { key: "Enter" });

    expect(onSubmit).toHaveBeenCalledOnce();
  });

  it("breaks the line on shift+Enter instead of sending", () => {
    const { box, onSubmit, type } = draw();
    type("ready");

    fireEvent.keyDown(box, { key: "Enter", shiftKey: true });

    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("does not send an Enter that is accepting an IME candidate", () => {
    const { box, onSubmit, type } = draw();
    type("日本");

    fireEvent.keyDown(box, { key: "Enter", isComposing: true });

    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("refuses a draft that is only whitespace, both ways in", () => {
    const { box, onSubmit, type } = draw();
    type("   ");

    fireEvent.keyDown(box, { key: "Enter" });
    expect(onSubmit).not.toHaveBeenCalled();

    expect((screen.getByLabelText("Send") as HTMLButtonElement).disabled).toBe(
      true,
    );
  });
});

describe("Composer attachments", () => {
  const PNG = { id: "a1", name: "shot.png", mimeType: "image/png", data: "cG5n", size: 3 };
  const ACCEPTS = ["image/png"] as const;

  function file(name = "shot.png", type = "image/png") {
    return new File(["bytes"], name, { type });
  }

  /** Mount with attachment plumbing, which the base helper deliberately omits. */
  function drawWithFiles(
    props: Partial<React.ComponentProps<typeof Composer>> = {},
  ) {
    const onAttach = vi.fn();
    const onRemove = vi.fn();
    const onSubmit = vi.fn();
    render(
      <Composer
        draft=""
        onDraft={vi.fn()}
        onSubmit={onSubmit}
        busy={false}
        onStop={vi.fn()}
        accepted={ACCEPTS}
        onAttach={onAttach}
        onRemove={onRemove}
        {...props}
      />,
    );
    return { onAttach, onRemove, onSubmit };
  }

  it("takes a pasted file", () => {
    const { onAttach } = drawWithFiles();

    fireEvent.paste(screen.getByLabelText("Message"), {
      clipboardData: { files: [file()] },
    });

    expect(onAttach).toHaveBeenCalledOnce();
    expect(onAttach.mock.calls[0][0][0].name).toBe("shot.png");
  });

  // Most pastes are text. Taking the event for those would break pasting
  // altogether, which is why the handler checks for files first.
  it("leaves a plain text paste alone", () => {
    const { onAttach } = drawWithFiles();

    fireEvent.paste(screen.getByLabelText("Message"), {
      clipboardData: { files: [] },
    });

    expect(onAttach).not.toHaveBeenCalled();
  });

  it("takes a dropped file", () => {
    const { onAttach } = drawWithFiles();

    fireEvent.drop(screen.getByLabelText("Message").closest("form")!, {
      dataTransfer: { files: [file()] },
    });

    expect(onAttach).toHaveBeenCalledOnce();
  });

  it("takes a picked file, and lets the same one be picked twice", () => {
    const { onAttach } = drawWithFiles();
    const picker = document.querySelector('input[type="file"]') as HTMLInputElement;

    fireEvent.change(picker, { target: { files: [file()] } });

    expect(onAttach).toHaveBeenCalledOnce();
    // Reset after the change, so picking the same file again still fires one —
    // an input that kept its value reports no change the second time.
    expect(picker.value).toBe("");
  });

  it("shows each attachment with a way to remove it", () => {
    const { onRemove } = drawWithFiles({ attachments: [PNG] });

    expect(screen.getByText("shot.png")).toBeTruthy();
    fireEvent.click(screen.getByLabelText("Remove shot.png"));

    expect(onRemove).toHaveBeenCalledWith("a1");
  });

  // "Look at this" with no words is a whole question.
  it("sends a message that is only a file", () => {
    const { onSubmit } = drawWithFiles({ attachments: [PNG] });

    const send = screen.getByLabelText("Send") as HTMLButtonElement;
    expect(send.disabled).toBe(false);
    fireEvent.click(send);

    expect(onSubmit).toHaveBeenCalledOnce();
  });

  // A screenshot that lands nowhere reads as a broken panel. Naming the model
  // says what to change.
  it("says so when the model reads no files, rather than doing nothing", () => {
    const { onAttach } = drawWithFiles({ accepted: [] });

    fireEvent.paste(screen.getByLabelText("Message"), {
      clipboardData: { files: [file()] },
    });

    expect(onAttach).not.toHaveBeenCalled();
    expect(screen.getByRole("status").textContent).toMatch(/does not accept attachments/);
  });

  // Shown and disabled rather than hidden: a missing button reads as a missing
  // feature, where one that is there with this title names the thing to change.
  it("offers the paperclip, titled with the reason, for a text-only model", () => {
    drawWithFiles({ accepted: [] });

    expect(screen.getByTitle(/does not accept attachments/)).toBeTruthy();
  });

  // A panel with no attachment plumbing at all is the shape every other caller
  // had before this existed, and must still render.
  it("renders without any attachment props", () => {
    render(
      <Composer draft="hi" onDraft={vi.fn()} onSubmit={vi.fn()} busy={false} onStop={vi.fn()} />,
    );
    expect(document.querySelector('input[type="file"]')).toBeNull();
  });
});
