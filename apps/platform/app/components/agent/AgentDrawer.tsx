"use client";

import { useEffect, useRef, useState } from "react";
import { usePathname, useRouter } from "next/navigation";
import { ArrowDown, MessageSquarePlus, Pin, PinOff, X } from "lucide-react";
import AgentMessage from "./AgentMessage";
import ContextGauge from "./ContextGauge";
import Composer from "./Composer";
import ConversationList from "./ConversationList";
import WorkingStatus from "./WorkingStatus";
import { useAgentChat } from "./useAgentChat";
import { useStickToBottom } from "./useStickToBottom";
import { MAX_WIDTH, MIN_WIDTH } from "./panelPrefs";

/** How far one arrow key nudges the panel edge. */
const KEY_STEP = 16;

/**
 * The conversation drawer: transcript, composer, and the navigation the agent asks
 * for.
 *
 * Full height down the right-hand side rather than a box in the corner. The old
 * panel was 26rem by 36rem, which is two or three tool chips and a paragraph — and
 * a run that calls a few tools and explains itself does not fit in that, so
 * everything interesting was permanently scrolled past.
 *
 * Floating, it overlays rather than pushing the page, and has no backdrop, because
 * he navigates: asking "take me to that deployment" and watching the page behind
 * the drawer change is the whole point, and a modal would break it. Pinned, it
 * docks and the page shrinks beside it — for the long session where the panel
 * covering a third of the editor stops being acceptable.
 *
 * It does not position itself: the shell that renders it owns that, so switching
 * between docked and floating is a class change on one wrapper rather than a move
 * through the tree — a move would remount this component and abort the answer
 * streaming into it.
 */
export default function AgentDrawer({
  userKey,
  onCollapse,
  docked,
  onToggleDock,
  width,
  onResize,
  onResizeEnd,
  onBusy,
}: {
  userKey: string;
  onCollapse: () => void;
  docked: boolean;
  onToggleDock: () => void;
  width: number;
  onResize: (width: number) => void;
  onResizeEnd: (width: number) => void;
  onBusy: (busy: boolean) => void;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const [draft, setDraft] = useState("");

  const chat = useAgentChat(userKey, pathname, (target) => {
    // The path was already checked against this app's shape when the frame was
    // parsed; router.push keeps it a client-side navigation, so the drawer and the
    // conversation in it survive the move.
    router.push(target.path);
  });

  // Reported upwards so the collapsed launcher can show he is still working. The
  // panel is hidden, not unmounted, when it is closed, so this keeps arriving.
  useEffect(() => onBusy(chat.busy), [chat.busy, onBusy]);

  const { ref: scroller, following, toBottom } = useStickToBottom(chat.turns);
  // The last *agent* turn, not the last turn. A message sent mid-answer is
  // appended while the run continues, so the end of the array is a question the
  // reader just typed — with no gauge on it and nothing for the status strip to
  // report, which would blank both at the moment there is most to say.
  const open = chat.turns.findLast((turn) => turn.role === "agent");

  const submit = () => {
    chat.send(draft);
    setDraft("");
  };

  // Tears down whatever a drag in progress installed. Held in a ref so that a
  // panel unmounted mid-drag — signing out, say — does not leave window listeners
  // behind that go on resizing a panel nobody can see.
  const endDrag = useRef<(() => void) | null>(null);
  useEffect(() => () => endDrag.current?.(), []);

  // Plain pointer events, the same drag the editor's settings panel uses. Width
  // lives in the shell (it sizes the wrapper); the final value is committed to
  // storage on release rather than on every move.
  function startResize(e: React.PointerEvent) {
    e.preventDefault();
    // A second pointerdown without an intervening pointerup should not stack a
    // second set of listeners on top of the first.
    endDrag.current?.();
    const startX = e.clientX;
    const startWidth = width;
    let last = width;
    const onMove = (ev: PointerEvent) => {
      // Dragging the left edge leftwards widens the panel.
      last = startWidth + (startX - ev.clientX);
      onResize(last);
    };
    // pointercancel as well as pointerup: an OS or browser gesture can take the
    // pointer away mid-drag, and listeners that outlive the gesture would go on
    // resizing the panel on any later mouse move.
    const finish = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", finish);
      window.removeEventListener("pointercancel", finish);
      endDrag.current = null;
      onResizeEnd(last);
    };
    endDrag.current = finish;
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", finish);
    window.addEventListener("pointercancel", finish);
  }

  // The same adjustment from the keyboard, for anyone not dragging anything.
  function keyResize(e: React.KeyboardEvent) {
    const step =
      e.key === "ArrowLeft" ? KEY_STEP : e.key === "ArrowRight" ? -KEY_STEP : 0;
    if (!step) return;
    e.preventDefault();
    const next = width + step;
    onResize(next);
    onResizeEnd(next);
  }

  return (
    <aside
      aria-label="Dr. Octo"
      className={`relative flex h-full w-full flex-col border-l border-black/10 bg-white dark:border-white/15 dark:bg-zinc-900 ${
        // A docked panel is part of the page; only a floating one casts a shadow.
        docked ? "" : "shadow-2xl"
      }`}
    >
      {/* Drag the left edge to resize, or nudge it with the arrow keys. */}
      <div
        role="separator"
        tabIndex={0}
        aria-orientation="vertical"
        aria-label="Resize the panel"
        aria-valuenow={width}
        aria-valuemin={MIN_WIDTH}
        aria-valuemax={MAX_WIDTH}
        onPointerDown={startResize}
        onKeyDown={keyResize}
        // touch-none so a drag on a touch screen resizes rather than scrolling
        // the page out from under the gesture.
        className="absolute inset-y-0 left-0 z-10 w-1 touch-none cursor-col-resize hover:bg-sky-500/40 focus-visible:bg-sky-500/60 focus-visible:outline-none"
      />

      <header className="flex items-center gap-2 border-b border-black/10 px-3 py-2 dark:border-white/10">
        <span className="shrink-0 text-sm font-semibold">Dr. Octo</span>
        {/* The conversation on screen, in the space the header has going spare.
            Nothing when it has no name yet: the runtime names one once there is
            something to name, and reports it — a placeholder in the meantime
            would be a label that changes under the reader. */}
        {chat.title && (
          <span className="min-w-0 flex-1 truncate text-xs text-zinc-500" title={chat.title}>
            {chat.title}
          </span>
        )}
        <div className="ml-auto flex shrink-0 items-center gap-2">
          {open?.context && <ContextGauge gauge={open.context} />}
          <ConversationList onOpen={chat.resume} />
          <button
            type="button"
            onClick={onToggleDock}
            title={docked ? "Float over the page" : "Dock beside the page"}
            aria-label={docked ? "Float over the page" : "Dock beside the page"}
            aria-pressed={docked}
            className="rounded-md p-1 text-zinc-500 hover:bg-black/[0.05] hover:text-zinc-800 dark:hover:bg-white/10 dark:hover:text-zinc-100"
          >
            {docked ? <PinOff size={15} /> : <Pin size={15} />}
          </button>
          <button
            type="button"
            onClick={chat.reset}
            title="New conversation"
            aria-label="New conversation"
            className="rounded-md p-1 text-zinc-500 hover:bg-black/[0.05] hover:text-zinc-800 dark:hover:bg-white/10 dark:hover:text-zinc-100"
          >
            <MessageSquarePlus size={15} />
          </button>
          <button
            type="button"
            onClick={onCollapse}
            title="Close"
            aria-label="Close"
            className="rounded-md p-1 text-zinc-500 hover:bg-black/[0.05] hover:text-zinc-800 dark:hover:bg-white/10 dark:hover:text-zinc-100"
          >
            <X size={15} />
          </button>
        </div>
      </header>

      <div className="relative flex min-h-0 flex-1 flex-col">
        <div
          ref={scroller}
          className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto px-3 py-3"
        >
          {chat.turns.length === 0 && <Empty />}

          {chat.turns.map((turn) => (
            <AgentMessage key={turn.id} turn={turn} onAuthorize={chat.authorize} />
          ))}

          {chat.error && <p className="text-xs text-red-500">{chat.error}</p>}
        </div>

        {/* Offered only when following is off, so it is a way back rather than a
            permanent control — and it is the only sign that scrolling away turned
            anything off. */}
        {!following && (
          <button
            type="button"
            onClick={toBottom}
            aria-label="Jump to the latest"
            className="absolute right-4 bottom-2 flex items-center gap-1 rounded-full border border-black/10 bg-white px-2 py-1 text-[11px] text-zinc-600 shadow-md dark:border-white/15 dark:bg-zinc-800 dark:text-zinc-300"
          >
            <ArrowDown size={12} />
            Latest
          </button>
        )}
      </div>

      <WorkingStatus turn={open} />
      <Composer
        draft={draft}
        onDraft={setDraft}
        onSubmit={submit}
        busy={chat.busy}
        onStop={chat.stop}
      />
    </aside>
  );
}

function Empty() {
  return (
    <div className="m-auto max-w-[22rem] text-center text-xs text-zinc-500">
      <p>
        Ask about this installation — an integration, a deployment that will not
        start, or a flow you are writing.
      </p>
      <p className="mt-2">He knows which page you are on, and can take you to another one.</p>
    </div>
  );
}
