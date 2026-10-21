"use client";

import { useState } from "react";
import { Plus } from "lucide-react";
import { paasBridge } from "../../desktop";
import { useAction } from "../useAction";
import { CARD, INPUT, MUTED, PRIMARY } from "../ui";
import ErrorLine from "./ErrorLine";

/**
 * Add a platform to work with: a name, its API's address, and optionally an API key
 * instead of signing in. Naming one that exists changes its address.
 */
export default function AddInstallation() {
  const paas = paasBridge();
  const { busy, error, run } = useAction();
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [key, setKey] = useState("");

  return (
    <form
      className={`${CARD} flex flex-col gap-2 p-4`}
      onSubmit={(e) => {
        e.preventDefault();
        if (!paas) return;
        void run(async () => {
          await paas.addProfile(name.trim(), url.trim(), key.trim() || undefined);
          setName("");
          setUrl("");
          setKey("");
        });
      }}
    >
      <div className="flex flex-wrap items-center gap-2">
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Name, e.g. prod" aria-label="Installation name" className={`${INPUT} w-36`} />
        <input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://api.example.com" aria-label="API address" className={`${INPUT} flex-1`} />
        <input type="password" value={key} onChange={(e) => setKey(e.target.value)} placeholder="API key (optional)" aria-label="API key" className={`${INPUT} w-44`} />
        <button type="submit" disabled={busy || !name.trim() || !url.trim()} className={PRIMARY}>
          <Plus size={14} /> Add
        </button>
      </div>
      <p className={MUTED}>Without a key, sign in to it afterwards with your browser.</p>
      <ErrorLine error={error} />
    </form>
  );
}
