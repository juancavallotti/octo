"use client";

import { useState } from "react";
import { KeyRound, LogIn, LogOut, Star, Trash2 } from "lucide-react";
import { paasBridge } from "../../desktop";
import type { Installation, PlatformState } from "../types";
import { useAction } from "../useAction";
import { DANGER, INPUT, MUTED, SECONDARY } from "../ui";
import ErrorLine from "./ErrorLine";
import IdentityDot from "./IdentityDot";

/** One installation: who it knows the user as, and signing in, keys, default and removal. */
export default function InstallationRow({
  installation: p,
  signingIn,
}: {
  installation: Installation;
  signingIn: PlatformState["signingIn"];
}) {
  const paas = paasBridge();
  const { busy, error, run } = useAction();
  const [key, setKey] = useState<string | null>(null);
  const { identity } = p;
  const waiting = signingIn?.profile === p.name;
  const who =
    identity.state === "signed-in"
      ? `${identity.name || identity.email}${identity.email && identity.name ? ` <${identity.email}>` : ""} · ${identity.roles.join(", ") || "no roles"}`
      : identity.state === "error"
        ? identity.message
        : identity.state === "checking"
          ? "Checking…"
          : "Not signed in";

  return (
    <li className="flex flex-col gap-2 px-4 py-3">
      <div className="flex items-center gap-3">
        <IdentityDot identity={identity} />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2 text-sm font-medium">
            {p.name}
            {p.current && <span className="rounded bg-sky-500/15 px-1.5 text-[11px] text-sky-700 dark:text-sky-300">default</span>}
          </div>
          <div className={`${MUTED} truncate`} title={`${p.url} · ${p.auth}`}>
            {p.url} · {who}
          </div>
        </div>
        {identity.state === "signed-in" && identity.credential === "oauth" ? (
          <button type="button" disabled={busy} onClick={() => paas && void run(() => paas.signOut(p.name))} className={SECONDARY}>
            <LogOut size={14} /> Sign out
          </button>
        ) : identity.state !== "signed-in" ? (
          <button type="button" disabled={busy || waiting} onClick={() => paas && void run(() => paas.signIn(p.name))} className={SECONDARY}>
            <LogIn size={14} /> {waiting ? "Waiting for your browser…" : "Sign in"}
          </button>
        ) : null}
        <button type="button" disabled={busy} onClick={() => setKey(key === null ? "" : null)} className={SECONDARY} title="Use an API key instead of signing in">
          <KeyRound size={14} />
        </button>
        {!p.current && (
          <button type="button" disabled={busy} onClick={() => paas && void run(() => paas.useProfile(p.name))} className={SECONDARY} title="Make it the CLI's default">
            <Star size={14} />
          </button>
        )}
        <button type="button" disabled={busy} onClick={() => paas && void run(() => paas.removeProfile(p.name))} className={DANGER} aria-label={`Remove ${p.name}`}>
          <Trash2 size={14} />
        </button>
      </div>
      {key !== null && (
        <form
          className="flex items-center gap-2 pl-5"
          onSubmit={(e) => {
            e.preventDefault();
            if (paas) void run(() => paas.setKey(p.name, key)).then(() => setKey(null));
          }}
        >
          <input type="password" autoFocus value={key} onChange={(e) => setKey(e.target.value)} placeholder="API key" aria-label="API key" className={`${INPUT} flex-1 font-mono`} />
          <button type="submit" disabled={busy || !key.trim()} className={SECONDARY}>
            Save key
          </button>
        </form>
      )}
      {waiting && signingIn && (
        <p className={`${MUTED} pl-5`}>
          No browser? Open <span className="select-all break-all font-mono">{signingIn.link}</span>
        </p>
      )}
      <ErrorLine error={error} />
    </li>
  );
}
