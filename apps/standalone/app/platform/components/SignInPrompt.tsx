"use client";

import { LogIn } from "lucide-react";
import { paasBridge } from "../../desktop";
import type { Installation, PlatformState } from "../types";
import { useAction } from "../useAction";
import { CARD, MUTED, PRIMARY } from "../ui";
import ErrorLine from "./ErrorLine";

/** An installation that does not know who the user is yet, and the way to tell it. */
export default function SignInPrompt({
  installation,
  signingIn,
}: {
  installation: Installation;
  signingIn: PlatformState["signingIn"];
}) {
  const { busy, error, run } = useAction();
  const paas = paasBridge();
  const waiting = signingIn?.profile === installation.name;
  const { identity } = installation;

  return (
    <div className={`${CARD} flex flex-col items-start gap-3 p-6`}>
      <p className="text-sm">
        {identity.state === "checking"
          ? `Checking who you are on ${installation.name}…`
          : identity.state === "error"
            ? `${installation.name} did not answer: ${identity.message}`
            : `Sign in to ${installation.name} (${installation.url}) to see its integrations.`}
      </p>
      {identity.state !== "checking" && (
        <button
          type="button"
          disabled={busy || waiting}
          onClick={() => paas && void run(() => paas.signIn(installation.name))}
          className={PRIMARY}
        >
          <LogIn size={14} /> {waiting ? "Waiting for your browser…" : "Sign in"}
        </button>
      )}
      {waiting && signingIn && (
        <p className={MUTED}>
          No browser? Open <span className="select-all break-all font-mono">{signingIn.link}</span>
        </p>
      )}
      <ErrorLine error={error} />
    </div>
  );
}
