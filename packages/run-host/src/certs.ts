import { execFile } from "node:child_process";
import { mkdtemp, readFile, rm } from "node:fs/promises";
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
/** Characters a subject or SAN entry may carry, so nothing reaches openssl's parsers as syntax. */
const SAFE_NAME = /^[A-Za-z0-9.*:-]+$/;

const defaultExec: ExecFile = promisify(execFile);

/** Whether `openssl` can be run on this host. */
export async function opensslAvailable(exec: ExecFile = defaultExec): Promise<boolean> {
  try {
    await exec(OPENSSL, ["version"]);
    return true;
  } catch {
    return false;
  }
}

/** The `openssl req` arguments that mint `req` into `certFile` and `keyFile`. */
export function selfSignedArgs(req: SelfSignedRequest, certFile: string, keyFile: string): string[] {
  const san = req.hosts.map((h) => (isIP(h) ? `IP:${h}` : `DNS:${h}`)).join(",");
  return [
    "req", "-x509", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256", "-nodes",
    "-days", String(req.days),
    "-subj", `/CN=${req.commonName}`,
    "-addext", `subjectAltName=${san}`,
    "-keyout", keyFile, "-out", certFile,
  ];
}

/** Rejects a request openssl would misread or that makes no usable certificate. */
export function validateRequest(req: SelfSignedRequest): void {
  if (!SAFE_NAME.test(req.commonName)) throw new Error(`invalid common name "${req.commonName}"`);
  if (req.hosts.length === 0) throw new Error("at least one host is required");
  for (const h of req.hosts) {
    if (!SAFE_NAME.test(h)) throw new Error(`invalid host "${h}"`);
  }
  if (!Number.isInteger(req.days) || req.days < 1 || req.days > MAX_DAYS) {
    throw new Error(`days must be between 1 and ${MAX_DAYS}`);
  }
}

/**
 * Mints a self-signed certificate (P-256 key) with `openssl` in a private temporary
 * directory, reads both PEMs back and removes the directory.
 */
export async function mintSelfSigned(
  req: SelfSignedRequest,
  exec: ExecFile = defaultExec,
): Promise<SelfSignedPair> {
  validateRequest(req);
  const dir = await mkdtemp(join(tmpdir(), "octo-cert-"));
  try {
    const certFile = join(dir, "tls.crt");
    const keyFile = join(dir, "tls.key");
    await exec(OPENSSL, selfSignedArgs(req, certFile, keyFile));
    const [cert, key] = await Promise.all([readFile(certFile, "utf8"), readFile(keyFile, "utf8")]);
    return { cert, key };
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
}
