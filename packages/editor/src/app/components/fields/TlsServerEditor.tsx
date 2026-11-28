"use client";

import { useEffect, useState, type ReactNode } from "react";
import { KeyRound } from "lucide-react";
import { fileOfNode } from "../../model/project";
import { useCertificates } from "../../providers/CertificateProvider";
import { useResourceStore } from "../../providers/ResourceStoreProvider";
import {
  useEditorState,
  useLatestEditorState,
  EditorActionType,
} from "../../state/editorState";
import { projectFiles } from "../../state/projectHandlers";
import { declareAllInProject, declaredNames } from "../resources/declarations";
import { parseDays, parseHosts, storeMintedPair, withMintedPair } from "./tlsServer";

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";
const DEFAULT_DAYS = 365;

/**
 * Editor for a connector's server-TLS settings (the `tls-server` field type): the
 * nested settings themselves, passed in as `children`, plus "Generate self-signed
 * certificate" when the host can mint one. Minting stores the pair as two template
 * resources, declares them, and points `certResource`/`keyResource` at them on the
 * setting `field` of the connection `connectionId`; without a connection there is
 * nothing to bind the pair to, so minting is not offered.
 */
export default function TlsServerEditor({
  connectionId,
  field,
  children,
}: {
  connectionId?: string;
  field: string;
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
        connectionId &&
        (open ? (
          <MintForm
            connectionId={connectionId}
            field={field}
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
  connectionId,
  field,
  onDone,
}: {
  connectionId: string;
  field: string;
  onDone: () => void;
}) {
  const minter = useCertificates();
  const store = useResourceStore();
  const { state, dispatch } = useEditorState();
  const latest = useLatestEditorState();
  const [commonName, setCommonName] = useState("localhost");
  const [hosts, setHosts] = useState("localhost, 127.0.0.1");
  const [days, setDays] = useState(String(DEFAULT_DAYS));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Minting awaits the host and the store, and the user can open another file or
  // connection meanwhile. The pair is therefore bound to where it started — this
  // project, the file holding the connection, the connection itself — and committed
  // against the project as it is when minting finishes, never a snapshot from before.
  const mint = async () => {
    if (!minter || !store) return;
    const validDays = parseDays(days);
    if (validDays === null) {
      setError("Valid for must be a whole number of days, at least 1.");
      return;
    }
    const origin = {
      projectId: state.project.id,
      path: fileOfNode(projectFiles(state), connectionId),
    };
    setBusy(true);
    setError(null);
    try {
      const cn = commonName.trim();
      const pair = await minter.mintSelfSigned({ commonName: cn, hosts: parseHosts(hosts), days: validDays });
      const stored = await storeMintedPair(store, cn, pair, () => declaredNames(projectFiles(latest())));

      const now = latest();
      const files = projectFiles(now);
      const connector =
        now.project.id === origin.projectId && fileOfNode(files, connectionId) === origin.path
          ? files
              .find((f) => f.path === origin.path)
              ?.document.connectors.find((c) => c.id === connectionId)
          : undefined;
      if (!connector) {
        // The connection is gone, so the pair has nothing to serve: take it back out.
        await Promise.allSettled([store.remove(stored.cert.id), store.remove(stored.key.id)]);
        setError("The connection was removed before the certificate was ready.");
        return;
      }
      const changes = declareAllInProject(files, [stored.names.cert, stored.names.key], "template");
      if (changes.length > 0) {
        dispatch({ type: EditorActionType.SET_RESOURCES, data: { changes } });
      }
      dispatch({
        type: EditorActionType.UPDATE_CONNECTION_SETTING,
        data: {
          id: connectionId,
          field,
          value: withMintedPair(connector.settings[field], stored.names),
        },
      });
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
          onChange={(e) => setDays(e.target.value)}
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
