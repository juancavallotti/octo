package standalone

import (
	"sync"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/certs"
)

var _ core.ServerTLSProvider = (*Services)(nil)

// serverTLS loads the process's serving certificate once, from the files named by
// OCTO_TLS_CERT_FILE and OCTO_TLS_KEY_FILE. The returned source follows renewals
// of those files.
var serverTLS = sync.OnceValues(certs.FromEnv)

// ServerCertificate returns the process's serving certificate, or nil when the
// environment names none.
func (s *Services) ServerCertificate() (*certs.Source, error) { return serverTLS() }
