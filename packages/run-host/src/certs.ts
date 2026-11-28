import { execFile } from "node:child_process";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { isIP } from "node:net";
import { join } from "node:path";
import { promisify } from "node:util";

/**
 * Self-signed certificates for a flow's HTTPS listener, minted with the host's own
 * `openssl`. There is deliberately no fallback: a host without `openssl` reports the
 * capability as unavailable rather than shipping a crypto implementation of its own.
 */

/** What to put on the certificate. */
export interface SelfSignedRequest {
  /** The subject common name, e.g. `localhost`. */
  commonName: string;
  /** DNS names or IP addresses the certificate is valid for. */
  hosts: string[];
  /** Validity in days. */
  days: number;
}

/** A PEM certificate and its PEM private key. */
export interface SelfSignedPair {
  cert: string;
  key: string;
}

/** The `execFile` shape these helpers need, so a test can stand in for `openssl`. */
export type ExecFile = (file: string, args: string[]) => Promise<unknown>;

const OPENSSL = "openssl";
const MAX_DAYS = 3650;
/** A DNS name, optionally a wildcard; no port, nothing openssl's parsers read as syntax. */
const DNS_NAME = /^(\*\.)?[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*$/;

/**
 * The whole openssl configuration a mint runs with. Passing it with `-config` keeps
 * the host's own `openssl.cnf` out of the certificate: some ship `v3_ca` as the
 * default x509 extensions, which would make the flow's server certificate a CA.
 * `leaf` is the profile `-extensions` selects; the SANs are added with `-addext`.
 */
export const LEAF_CONFIG = `[req]
distinguished_name = dn
[dn]
[leaf]
basicConstraints = critical,CA:FALSE
keyUsage = critical,digitalSignature,keyEncipherment
extendedKeyUsage = serverAuth
subjectKeyIdentifier = hash
`;

const defaultExec: ExecFile = promisify(execFile);

/** What a run printed, from its result or from the error a non-zero exit rejects with. */
function printed(out: unknown): string {
  const o = (out ?? {}) as { stdout?: unknown; stderr?: unknown };
  return `${o.stdout ?? ""}${o.stderr ?? ""}`;
}

/**
 * Whether this host's `openssl` can mint: it runs, and its `req` lists `-addext`
 * (OpenSSL 1.1.1 and later, LibreSSL 3.1 and later), which the mint relies on. The
 * usage text is read whatever the exit status, because LibreSSL exits non-zero after
 * printing it.
 */
export async function opensslAvailable(exec: ExecFile = defaultExec): Promise<boolean> {
  let usage: string;
  try {
    usage = printed(await exec(OPENSSL, ["req", "-help"]));
  } catch (err) {
    usage = printed(err);
  }
  return usage.includes("-addext");
}

/** The `openssl req` arguments that mint `req` into `certFile` and `keyFile`, configured by `configFile`. */
export function selfSignedArgs(
  req: SelfSignedRequest,
  configFile: string,
  certFile: string,
  keyFile: string,
): string[] {
  const san = req.hosts.map((h) => (isIP(h) ? `IP:${h}` : `DNS:${h}`)).join(",");
  return [
    "req", "-x509", "-config", configFile, "-extensions", "leaf",
    "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256", "-nodes",
    "-days", String(req.days),
    "-subj", `/CN=${req.commonName}`,
    "-addext", `subjectAltName=${san}`,
    "-keyout", keyFile, "-out", certFile,
  ];
}

const validName = (name: string) => isIP(name) !== 0 || DNS_NAME.test(name);

/**
 * Validates `req` and returns the validated copy, so nothing that changes `req`
 * afterwards reaches openssl. Rejects what openssl would misread or what makes no
 * usable certificate, a host with a port among them.
 */
export function validateRequest(req: SelfSignedRequest): SelfSignedRequest {
  const ret: SelfSignedRequest = { commonName: req.commonName, hosts: [...req.hosts], days: req.days };
  if (!validName(ret.commonName)) throw new Error(`invalid common name "${ret.commonName}"`);
  if (ret.hosts.length === 0) throw new Error("at least one host is required");
  for (const h of ret.hosts) {
    if (!validName(h)) throw new Error(`invalid host "${h}": use a DNS name or an IP address, without a port`);
  }
  if (!Number.isInteger(ret.days) || ret.days < 1 || ret.days > MAX_DAYS) {
    throw new Error(`days must be between 1 and ${MAX_DAYS}`);
  }
  return ret;
}

/**
 * Mints a self-signed server certificate (P-256 key) with `openssl` in a private
 * temporary directory, reads both PEMs back and removes the directory.
 */
export async function mintSelfSigned(
  req: SelfSignedRequest,
  exec: ExecFile = defaultExec,
): Promise<SelfSignedPair> {
  const checked = validateRequest(req);
  const dir = await mkdtemp(join(tmpdir(), "octo-cert-"));
  try {
    const configFile = join(dir, "openssl.cnf");
    const certFile = join(dir, "tls.crt");
    const keyFile = join(dir, "tls.key");
    await writeFile(configFile, LEAF_CONFIG, "utf8");
    await exec(OPENSSL, selfSignedArgs(checked, configFile, certFile, keyFile));
    const [cert, key] = await Promise.all([readFile(certFile, "utf8"), readFile(keyFile, "utf8")]);
    return { cert, key };
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
}
