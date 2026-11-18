package http

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/certs"
)

const (
	// envHTTPSPort is the unprefixed env var naming the HTTPS port. When it is set
	// and a certificate is found, the connector serves HTTPS on it instead of plain
	// HTTP on its port, even without a tls block.
	envHTTPSPort = "HTTPS_PORT"

	tlsVersion12 = "1.2"
	tlsVersion13 = "1.3"
)

// tlsSettings configures HTTPS. At most one certificate source may be set; with
// none, the certificate comes from the runtime services.
type tlsSettings struct {
	// Serve TLS. Implied when any certificate source below is set.
	Enabled bool `json:"enabled" octo:"label=Enabled"`
	// Second, HTTPS-only port. When set, HTTPS listens here next to plain HTTP on the
	// main port; when unset, the main port speaks TLS.
	Port *int `json:"port" octo:"label=HTTPS port"`
	// PEM certificate chain, usually ${TLS_CERT}.
	Cert string `json:"cert" octo:"label=Certificate (PEM)"`
	// PEM private key, usually ${TLS_KEY}.
	Key string `json:"key" octo:"label=Private key (PEM)"`
	// Path to a PEM certificate chain; a renewed file is picked up without a restart.
	CertFile string `json:"certFile" octo:"label=Certificate file"`
	// Path to a PEM private key; a renewed file is picked up without a restart.
	KeyFile string `json:"keyFile" octo:"label=Private key file"`
	// Resource holding the PEM certificate chain.
	CertResource string `json:"certResource" octo:"label=Certificate resource,ref=template"`
	// Resource holding the PEM private key.
	KeyResource string `json:"keyResource" octo:"label=Private key resource,ref=template"`
	// Lowest TLS version accepted.
	MinVersion string `json:"minVersion" octo:"label=Minimum TLS version,type=enum,enum=1.2|1.3,default=1.2"`
}

// tlsPlan is the resolved HTTPS setup: the certificate, and whether it is served
// on the main port or on a second one.
type tlsPlan struct {
	config *tls.Config
	// port is the second, HTTPS-only port; nil means the main port speaks TLS.
	port *int
	// mainPort, from $HTTPS_PORT, moves the main listener there: the connector then
	// serves HTTPS on it and opens no plain HTTP port at all.
	mainPort *int
}

// resolveTLS decides how the connector serves HTTPS, or returns nil when it serves
// plain HTTP only. A tls block that asks for TLS (enabled, a port, or a source) and
// cannot find a certificate is an error; $HTTPS_PORT without a provisioned
// certificate is not, since the variable alone says nothing about the flow's intent.
func resolveTLS(ctx context.Context, set tlsSettings) (*tlsPlan, error) {
	explicit := set.Enabled || set.Port != nil || set.hasSource()
	var mainPort *int
	if set.Port == nil {
		mainPort = envHTTPSPortValue()
	}
	if !explicit && mainPort == nil {
		return nil, nil
	}
	src, err := tlsSource(ctx, set)
	if err != nil {
		return nil, err
	}
	if src == nil {
		if explicit {
			return nil, errors.New("tls is enabled but no certificate is set and the runtime provides none")
		}
		slog.Warn("http connector: " + envHTTPSPort + " is set but no certificate is available; serving HTTP only")
		return nil, nil
	}
	minVersion, err := tlsMinVersion(set.MinVersion)
	if err != nil {
		return nil, err
	}
	return &tlsPlan{
		config:   &tls.Config{MinVersion: minVersion, GetCertificate: src.GetCertificate},
		port:     set.Port,
		mainPort: mainPort,
	}, nil
}

func (s tlsSettings) hasSource() bool {
	return s.Cert != "" || s.Key != "" || s.CertFile != "" || s.KeyFile != "" ||
		s.CertResource != "" || s.KeyResource != ""
}

// tlsSource returns the certificate from the one source the settings name, or
// from the runtime services when they name none. It returns nil when neither has
// a certificate.
func tlsSource(ctx context.Context, set tlsSettings) (*certs.Source, error) {
	inline := set.Cert != "" || set.Key != ""
	file := set.CertFile != "" || set.KeyFile != ""
	resource := set.CertResource != "" || set.KeyResource != ""
	if countTrue(inline, file, resource) > 1 {
		return nil, errors.New("tls takes one certificate source: cert/key, certFile/keyFile or certResource/keyResource")
	}
	switch {
	case inline:
		if err := requirePair("cert", set.Cert, "key", set.Key); err != nil {
			return nil, err
		}
		return certs.FromPEM([]byte(set.Cert), []byte(set.Key))
	case file:
		if err := requirePair("certFile", set.CertFile, "keyFile", set.KeyFile); err != nil {
			return nil, err
		}
		return certs.FromFiles(set.CertFile, set.KeyFile)
	case resource:
		return resourceSource(ctx, set.CertResource, set.KeyResource)
	}
	if provider, ok := core.RuntimeServicesFromContext(ctx).(core.ServerTLSProvider); ok {
		return provider.ServerCertificate()
	}
	return nil, nil
}

func resourceSource(ctx context.Context, certID, keyID string) (*certs.Source, error) {
	if err := requirePair("certResource", certID, "keyResource", keyID); err != nil {
		return nil, err
	}
	svc := core.RuntimeServicesFromContext(ctx)
	if svc == nil {
		return nil, errors.New("tls certResource needs a resource loader")
	}
	certPEM, err := svc.Resources().Load(ctx, core.ResourceKindTemplate, certID)
	if err != nil {
		return nil, fmt.Errorf("load certResource %q: %w", certID, err)
	}
	keyPEM, err := svc.Resources().Load(ctx, core.ResourceKindTemplate, keyID)
	if err != nil {
		return nil, fmt.Errorf("load keyResource %q: %w", keyID, err)
	}
	return certs.FromPEM(certPEM, keyPEM)
}

func requirePair(certName, certVal, keyName, keyVal string) error {
	if certVal == "" || keyVal == "" {
		return fmt.Errorf("tls %s and %s must be set together", certName, keyName)
	}
	return nil
}

func tlsMinVersion(v string) (uint16, error) {
	switch v {
	case "", tlsVersion12:
		return tls.VersionTLS12, nil
	case tlsVersion13:
		return tls.VersionTLS13, nil
	}
	return 0, fmt.Errorf("tls minVersion %q is not one of %s or %s", v, tlsVersion12, tlsVersion13)
}

// envHTTPSPortValue reads $HTTPS_PORT, or nil when it is unset. An unparseable
// value is logged and ignored, as $HTTP_PORT is.
func envHTTPSPortValue() *int {
	env, ok := os.LookupEnv(envHTTPSPort)
	if !ok || env == "" {
		return nil
	}
	p, err := strconv.Atoi(env)
	if err != nil {
		slog.Warn("http connector: ignoring unparseable "+envHTTPSPort, "value", env)
		return nil
	}
	return &p
}

func countTrue(flags ...bool) int {
	n := 0
	for _, f := range flags {
		if f {
			n++
		}
	}
	return n
}
