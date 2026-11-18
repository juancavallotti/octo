"use client";

import { createContext, useContext, type ReactNode } from "react";

/**
 * The editor's certificate capability: minting a self-signed certificate for a
 * connector's TLS settings. A host offers it when it can mint one on its machine —
 * typically by shelling out to `openssl` — and reports through `available` whether it
 * actually can right now. When it is null, or unavailable, the TLS settings show no
 * minting action.
 */

/** What to put on a minted certificate. */
export interface MintCertificateRequest {
  /** The subject common name, e.g. `localhost`. */
  commonName: string;
  /** DNS names or IP addresses the certificate is valid for. */
  hosts: string[];
  /** Validity in days. */
  days: number;
}

/** A minted PEM certificate and its PEM private key. */
export interface MintedCertificate {
  cert: string;
  key: string;
}

export interface CertificateMinter {
  /** Whether minting works on this host right now. */
  available(): Promise<boolean>;
  /** Mint a self-signed certificate. */
  mintSelfSigned(req: MintCertificateRequest): Promise<MintedCertificate>;
}

const CertificateContext = createContext<CertificateMinter | null>(null);

export function CertificateProvider({
  value,
  children,
}: {
  value: CertificateMinter | null;
  children: ReactNode;
}) {
  return (
    <CertificateContext.Provider value={value}>
      {children}
    </CertificateContext.Provider>
  );
}

/** The current certificate minter, or null when the host offers none. */
export function useCertificates(): CertificateMinter | null {
  return useContext(CertificateContext);
}
