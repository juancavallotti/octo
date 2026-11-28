import type { MintedCertificate } from "../../providers/CertificateProvider";
import type { ResourceStore, StoredResource } from "../../providers/ResourceStoreProvider";

/** Resource names for a minted certificate and its key. */
export interface MintedNames {
  cert: string;
  key: string;
}

/** The certificate sources a tls-server group can name; minting replaces them all. */
const SOURCE_KEYS = ["cert", "key", "certFile", "keyFile", "certResource", "keyResource"];

/** Splits a comma-separated host list, dropping blanks. */
export function parseHosts(raw: string): string[] {
  return raw
    .split(",")
    .map((h) => h.trim())
    .filter((h) => h !== "");
}

/**
 * Names for a pair minted for `commonName` under `tls/`, numbered past any name in
 * `existing` (stored or declared) so minting never overwrites or shadows one.
 */
export function mintedNames(commonName: string, existing: readonly string[]): MintedNames {
  const base = `tls/${commonName.replace(/[^A-Za-z0-9.-]/g, "_") || "cert"}`;
  const taken = new Set(existing);
  for (let n = 1; ; n++) {
    const stem = n === 1 ? base : `${base}-${n}`;
    const names = { cert: `${stem}.crt`, key: `${stem}.key` };
    if (!taken.has(names.cert) && !taken.has(names.key)) return names;
  }
}

/** The tls-server group with its certificate source replaced by the minted pair. */
export function withMintedPair(value: unknown, names: MintedNames): Record<string, unknown> {
  const group = value && typeof value === "object" ? { ...(value as Record<string, unknown>) } : {};
  for (const k of SOURCE_KEYS) delete group[k];
  return { ...group, certResource: names.cert, keyResource: names.key };
}

/** The validity entered in days, or null unless it is a whole number of at least 1. */
export function parseDays(raw: string): number | null {
  const trimmed = raw.trim();
  if (!/^\d+$/.test(trimmed)) return null;
  const days = Number(trimmed);
  return days >= 1 ? days : null;
}

/** A minted pair as stored: the names chosen and the two stored records. */
export interface StoredPair {
  names: MintedNames;
  cert: StoredResource;
  key: StoredResource;
}

const STORE_ATTEMPTS = 5;

/**
 * Store a minted pair as two template resources under free names. A name counts as
 * taken when it is stored or in `reserved` (the project's declared names). Another
 * mint can claim a name between the listing and the write; a store that refuses to
 * overwrite then fails the write, and when the name is now listed the pair moves on
 * to the next suffix. The certificate is removed again when its key cannot be stored,
 * so a failure never leaves half a pair behind.
 */
export async function storeMintedPair(
  store: ResourceStore,
  commonName: string,
  pair: MintedCertificate,
  reserved: () => Iterable<string>,
): Promise<StoredPair> {
  const listed = async (name: string) => (await store.list()).some((r) => r.name === name);
  for (let attempt = 0; attempt < STORE_ATTEMPTS; attempt++) {
    const stored = (await store.list()).map((r) => r.name);
    const names = mintedNames(commonName, [...stored, ...reserved()]);
    let cert: StoredResource;
    try {
      cert = await store.create({ name: names.cert, kind: "template", content: pair.cert });
    } catch (e) {
      if (await listed(names.cert)) continue;
      throw e;
    }
    try {
      const key = await store.create({ name: names.key, kind: "template", content: pair.key });
      return { names, cert, key };
    } catch (e) {
      await store.remove(cert.id).catch(() => undefined);
      if (await listed(names.key)) continue;
      throw e;
    }
  }
  throw new Error("Another certificate kept taking the same name. Try generating again.");
}
