import type { CertificateMinter } from "@octo/editor";
import { certificatesAvailable, mintCertificate } from "../actions/certificates";
import { unwrap } from "../actions/result";

/** The local certificate capability: minting runs on this machine's `openssl`. */
export const localCertificateMinter: CertificateMinter = {
  async available() {
    return unwrap(await certificatesAvailable());
  },
  async mintSelfSigned(req) {
    return unwrap(await mintCertificate(req));
  },
};
