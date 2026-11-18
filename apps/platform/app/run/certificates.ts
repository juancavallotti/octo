import type { CertificateMinter } from "@octo/editor";
import { certificatesAvailable, mintCertificate } from "@/app/actions/certificates";
import { unwrap } from "@/app/model/bff";

/**
 * The platform's certificate capability: minting runs in the platform server, so it
 * is available only when that server's image carries `openssl`.
 */
export const bffCertificateMinter: CertificateMinter = {
  async available() {
    return unwrap(await certificatesAvailable());
  },
  async mintSelfSigned(req) {
    return unwrap(await mintCertificate(req));
  },
};
