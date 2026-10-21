"use client";

import { useState } from "react";
import { Rocket } from "lucide-react";
import { paasBridge } from "../../desktop";
import type { Binding, VersionSummary } from "../types";
import { useAction } from "../useAction";
import { CARD, INPUT, MUTED, PRIMARY } from "../ui";
import BindingRows from "./BindingRows";
import ErrorLine from "./ErrorLine";

/**
 * A new deployment. Without a version octopaas deploys the draft as it stands, frozen
 * under the next tag — so by default the folder is pushed first, and what runs is
 * what the editor shows.
 */
export default function DeployForm({ versions, busy: running }: { versions: VersionSummary[]; busy: boolean }) {
  const paas = paasBridge();
  const { busy, error, run } = useAction();
  const [version, setVersion] = useState("");
  const [pushFirst, setPushFirst] = useState(true);
  const [replicas, setReplicas] = useState("");
  const [env, setEnv] = useState<Binding[]>([]);
  const [secrets, setSecrets] = useState<Binding[]>([]);
  const [tracing, setTracing] = useState(false);
  const [subdomain, setSubdomain] = useState("");

  const submit = () =>
    paas &&
    void run(() =>
      paas.deploy(
        {
          ...(version ? { version } : {}),
          env,
          secrets,
          ...(tracing ? { tracing } : {}),
          ...(replicas !== "" ? { replicas: Number(replicas) } : {}),
          ...(subdomain ? { subdomain } : {}),
        },
        pushFirst,
      ),
    );

  return (
    <div className={`${CARD} flex flex-col gap-3 p-4`}>
      <div className="flex flex-wrap items-center gap-2">
        <select value={version} onChange={(e) => setVersion(e.target.value)} aria-label="Version" className={INPUT}>
          <option value="">The folder as it is now</option>
          {versions.map((v) => (
            <option key={v.id} value={v.tag}>
              {v.tag}
            </option>
          ))}
        </select>
        {!version && (
          <label className="flex items-center gap-1.5 text-sm">
            <input type="checkbox" checked={pushFirst} onChange={(e) => setPushFirst(e.target.checked)} />
            Push it first
          </label>
        )}
        <input type="number" min={0} max={100} value={replicas} onChange={(e) => setReplicas(e.target.value)} placeholder="Replicas" aria-label="Replicas" className={`${INPUT} w-24`} />
        <label className="flex items-center gap-1.5 text-sm">
          <input type="checkbox" checked={tracing} onChange={(e) => setTracing(e.target.checked)} />
          Tracing
        </label>
      </div>
      <BindingRows label="Env vars" valueLabel="value" rows={env} onChange={setEnv} />
      <BindingRows label="From cluster secrets" valueLabel="secret name" rows={secrets} onChange={setSecrets} />
      <div className="flex flex-wrap items-center gap-2">
        <input value={subdomain} onChange={(e) => setSubdomain(e.target.value)} placeholder="Subdomain, to publish it outside the cluster" aria-label="Subdomain" className={`${INPUT} flex-1`} />
        <button type="button" disabled={busy || running || !paas} onClick={submit} className={PRIMARY}>
          <Rocket size={14} /> Deploy
        </button>
      </div>
      <p className={MUTED}>Every env var the integration declares needs a value or a secret. The deploy waits until it is running.</p>
      <ErrorLine error={error} />
    </div>
  );
}
