"use client";

import { useEffect, useState, type ReactNode } from "react";
import { KeyRound } from "lucide-react";
import { useCertificates } from "../../providers/CertificateProvider";
import { useResourceStore } from "../../providers/ResourceStoreProvider";
import { useEditorState, EditorActionType } from "../../state/editorState";
import { useProjectFiles } from "../../state/useProject";
import { declareAllInProject } from "../resources/declarations";
import { mintedNames, parseHosts, withMintedPair } from "./tlsServer";

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";
const DEFAULT_DAYS = 365;

/**
 * Editor for a connector's server-TLS settings (the `tls-server` field type): the
 * nested settings themselves, passed in as `children`, plus "Generate self-signed
 * certificate" when the host can mint one. Minting stores the pair as two template
 * resources, declares them, and points `certResource`/`keyResource` at them.
 */
export default function TlsServerEditor({
  value,
  onChange,
  children,
}: {
  value: unknown;
  onChange: (value: unknown) => void;
  children: ReactNode;
}) {
  const minter = useCertificates();
  const store = useResourceStore();
  const [available, setAvailable] = useState(false);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (!minter || !store) return;
    let cancelled = false;
    minter
      .available()
      .then((ok) => !cancelled && setAvailable(ok))
      .catch(() => !cancelled && setAvailable(false));
    return () => {
      cancelled = true;
    };
  }, [minter, store]);

  return (
    <div className="flex flex-col gap-2">
      {children}
      {available &&
        (open ? (
          <MintForm
            value={value}
            onChange={onChange}
            onDone={() => setOpen(false)}
          />
        ) : (
          <button
            type="button"
            onClick={() => setOpen(true)}
            className="flex items-center gap-1.5 self-start rounded-md px-2 py-1 text-xs text-sky-600 hover:bg-sky-500/10 dark:text-sky-400"
          >
            <KeyRound size={12} />
            Generate self-signed certificate
          </button>
        ))}
    </div>
  );
}

function MintForm({
  value,
  onChange,
  onDone,
}: {
  value: unknown;
  onChange: (value: unknown) => void;
  onDone: () => void;
}) {
  const minter = useCertificates();
  const store = useResourceStore();
  const { dispatch } = useEditorState();
  const files = useProjectFiles();
  const [commonName, setCommonName] = useState("localhost");
  const [hosts, setHosts] = useState("localhost, 127.0.0.1");
  const [days, setDays] = useState(DEFAULT_DAYS);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const mint = async () => {
    if (!minter || !store) return;
    setBusy(true);
    setError(null);
    try {
      const pair = await minter.mintSelfSigned({
        commonName: commonName.trim(),
        hosts: parseHosts(hosts),
        days,
      });
      const existing = (await store.list()).map((r) => r.name);
      const names = mintedNames(commonName.trim(), existing);
      await store.create({ name: names.cert, kind: "template", content: pair.cert });
      await store.create({ name: names.key, kind: "template", content: pair.key });
      const changes = declareAllInProject(files, [names.cert, names.key], "template");
      if (changes.length > 0) {
        dispatch({ type: EditorActionType.SET_RESOURCES, data: { changes } });
      }
      onChange(withMintedPair(value, names));
      onDone();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex flex-col gap-2 rounded-md border border-dashed border-black/10 p-2 text-xs dark:border-white/15">
      <label className="flex flex-col gap-1">
        Common name
        <input className={INPUT} value={commonName} onChange={(e) => setCommonName(e.target.value)} />
      </label>
      <label className="flex flex-col gap-1">
        Hosts (comma-separated DNS names or IPs)
        <input className={INPUT} value={hosts} onChange={(e) => setHosts(e.target.value)} />
      </label>
      <label className="flex flex-col gap-1">
        Valid for (days)
        <input
          className={INPUT}
          type="number"
          min={1}
          value={days}
          onChange={(e) => setDays(Number(e.target.value) || DEFAULT_DAYS)}
        />
      </label>
      {error && <p className="text-red-500">{error}</p>}
      <div className="flex justify-end gap-2">
        <button type="button" onClick={onDone} disabled={busy} className="rounded-md px-2 py-1 hover:bg-black/[0.06] dark:hover:bg-white/[0.08]">
          Cancel
        </button>
        <button
          type="button"
          onClick={mint}
          disabled={busy || commonName.trim() === ""}
          className="rounded-md bg-sky-500 px-2 py-1 text-white disabled:opacity-50"
        >
          {busy ? "Generating…" : "Generate"}
        </button>
      </div>
    </div>
  );
}
