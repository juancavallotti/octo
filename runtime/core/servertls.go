package core

import "github.com/juancavallotti/octo/runtime/core/certs"

// ServerTLSProvider is an optional capability a RuntimeServices implementation may
// provide: the certificate this runtime presents when it serves TLS. Callers
// type-assert the active RuntimeServices to ServerTLSProvider. ServerCertificate
// returns nil and no error when the runtime has no certificate provisioned, so
// callers MUST nil-check the source.
type ServerTLSProvider interface {
	ServerCertificate() (*certs.Source, error)
}
