package standalone

import (
	"sync"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/certs"
)

var _ core.ServerTLSProvider = (*Services)(nil)

// serverTLS holds the process's serving certificate, loaded from the files named
// by OCTO_TLS_CERT_FILE and OCTO_TLS_KEY_FILE. Only a successful load is kept: a
// failed one is retried on the next call, so fixing the files recovers without a
// restart. The kept source follows renewals of those files on its own.
var serverTLS struct {
	mu     sync.Mutex
	loaded bool
	source *certs.Source
}

// ServerCertificate returns the process's serving certificate, or nil when the
// environment names none.
func (s *Services) ServerCertificate() (*certs.Source, error) {
	serverTLS.mu.Lock()
	defer serverTLS.mu.Unlock()
	if serverTLS.loaded {
		return serverTLS.source, nil
	}
	src, err := certs.FromEnv()
	if err != nil {
		return nil, err
	}
	serverTLS.loaded, serverTLS.source = true, src
	return src, nil
}
