package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

const (
	// envHTTPSPort turns HTTPS on beside the plain server; OCTO_TLS_CERT_FILE and
	// OCTO_TLS_KEY_FILE name the certificate it serves.
	envHTTPSPort = "HTTPS_PORT"
	envCertFile  = "OCTO_TLS_CERT_FILE"
	envKeyFile   = "OCTO_TLS_KEY_FILE"
	// certCheckInterval bounds how often the certificate files are read, so a
	// busy listener does not read them on every handshake.
	certCheckInterval = 10 * time.Second
	// certWarnInterval bounds how often a failing reload is logged while the
	// previous pair stays in service.
	certWarnInterval = time.Minute
)

// httpsFromEnv returns a server that serves plain's handler over HTTPS on
// $HTTPS_PORT, with the certificate named by OCTO_TLS_CERT_FILE and
// OCTO_TLS_KEY_FILE, or nil when HTTPS_PORT is unset. The certificate is re-read
// when its files change, so a renewal needs no restart. Start it with
// ListenAndServeTLS("", "").
func httpsFromEnv(plain *http.Server) (*http.Server, error) {
	port := os.Getenv(envHTTPSPort)
	if port == "" {
		return nil, nil
	}
	cert, err := loadCert(os.Getenv(envCertFile), os.Getenv(envKeyFile))
	if err != nil {
		return nil, err
	}
	return &http.Server{
		Addr:              ":" + port,
		Handler:           plain.Handler,
		ReadHeaderTimeout: plain.ReadHeaderTimeout,
		ReadTimeout:       plain.ReadTimeout,
		WriteTimeout:      plain.WriteTimeout,
		IdleTimeout:       plain.IdleTimeout,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: cert.get},
	}, nil
}

// shutdown shuts every non-nil server down at once under ctx and joins their
// errors, so no listener keeps accepting while another drains.
func shutdown(ctx context.Context, servers ...*http.Server) error {
	errs := make([]error, len(servers))
	var wg sync.WaitGroup
	for i, srv := range servers {
		if srv == nil {
			continue
		}
		wg.Go(func() { errs[i] = srv.Shutdown(ctx) })
	}
	wg.Wait()
	return errors.Join(errs...)
}

// reloadingCert holds a certificate pair loaded from disk. Every
// certCheckInterval it reads both files again and, when their bytes differ from
// the pair in service, parses those same bytes and swaps them in. A pair that
// fails to parse — caught mid-rotation, or a certificate that does not match its
// key — leaves the previous pair in service, logs a warning at most every
// certWarnInterval, and is retried at the next check.
type reloadingCert struct {
	certFile, keyFile string
	now               func() time.Time

	mu        sync.Mutex
	cert      *tls.Certificate
	certPEM   []byte
	keyPEM    []byte
	checkedAt time.Time
	warnedAt  time.Time
}

func loadCert(certFile, keyFile string) (*reloadingCert, error) {
	if certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("%s is set, so %s and %s must be too", envHTTPSPort, envCertFile, envKeyFile)
	}
	c := &reloadingCert{certFile: certFile, keyFile: keyFile, now: time.Now}
	if err := c.reload(); err != nil {
		return nil, err
	}
	c.checkedAt = c.now()
	return c, nil
}

func (c *reloadingCert) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if now.Sub(c.checkedAt) >= certCheckInterval {
		c.checkedAt = now
		if err := c.reload(); err != nil && now.Sub(c.warnedAt) >= certWarnInterval {
			c.warnedAt = now
			slog.Warn("could not reload the HTTPS certificate; serving the previous one", "error", err)
		}
	}
	return c.cert, nil
}

func (c *reloadingCert) reload() error {
	certPEM, certErr := os.ReadFile(c.certFile)
	keyPEM, keyErr := os.ReadFile(c.keyFile)
	if err := errors.Join(certErr, keyErr); err != nil {
		return fmt.Errorf("read certificate files: %w", err)
	}
	if c.cert != nil && bytes.Equal(certPEM, c.certPEM) && bytes.Equal(keyPEM, c.keyPEM) {
		return nil
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("load certificate %s and key %s: %w", c.certFile, c.keyFile, err)
	}
	c.cert, c.certPEM, c.keyPEM = &pair, certPEM, keyPEM
	return nil
}
