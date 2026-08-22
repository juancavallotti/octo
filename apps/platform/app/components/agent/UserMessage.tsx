"use client";

import { Check, Loader2, MessageSquareOff } from "lucide-react";
import { answerOf, type Turn } from "./turns";

/**
 * What somebody said, and — when they said it mid-answer — whether he has read it
 * yet.
 *
 * A message sent while a run is in flight is not answered by the request that
 * carried it: the runtime hands it to the run already working and returns nothing,
 * so from this window it looks identical whether it was folded into the
 * conversation or thrown away. The run says which, on the stream that is already
 * open, and that is what these three states are.
 *
 * An ordinary question has no state to show. It started its own run, and the
 * answer appearing underneath it is the acknowledgement.
 */
export default function UserMessage({ turn }: { turn: Turn }) {
  return (
    <div className="flex flex-col items-end gap-0.5">
      <div
        className={`max-w-[85%] rounded-lg rounded-br-sm px-3 py-1.5 text-sm text-white transition-colors ${
          // Held back until he picks it up, so a message waiting on a model call
          // does not sit there looking as settled as one he has already answered.
          turn.delivery === "pending" ? "bg-sky-600/50" : "bg-sky-600"
        }`}
      >
        {answerOf(turn)}
      </div>
      <DeliveryNote delivery={turn.delivery} />
    </div>
  );
}

function DeliveryNote({ delivery }: { delivery: Turn["delivery"] }) {
  switch (delivery) {
    case "pending":
      return (
        <Note>
          <Loader2 size={11} className="animate-spin" />
          Waiting for him to read this
        </Note>
      );

    // He is mid-turn and cannot reply to it separately, so the only honest report
    // is that it is now part of what he is working on.
    case "taken":
      return (
        <Note>
          <Check size={11} />
          Read, and taken into account
        </Note>
      );

    // The one case where something a person sent goes nowhere — he was stopped, or
    // ran out of steps before reaching it. It must not be silent.
    case "missed":
      return (
        <Note tone="text-amber-600 dark:text-amber-400">
          <MessageSquareOff size={11} />
          He never got to this one. Ask again.
        </Note>
      );

    default:
      return null;
  }
}

function Note({
  children,
  tone = "text-zinc-500 dark:text-zinc-400",
}: {
  children: React.ReactNode;
  tone?: string;
}) {
  return <span className={`flex items-center gap-1 pr-0.5 text-[11px] ${tone}`}>{children}</span>;
}
