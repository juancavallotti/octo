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
 * Names for a pair minted for `commonName` under `tls/`, numbered past any pair of
 * the same name already stored so minting never overwrites one.
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
