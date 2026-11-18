package main

import (
	"crypto/tls"
	"errors"
	"fmt"
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
	// certCheckInterval bounds how often the certificate files are stat'ed, so a
	// busy listener does not stat on every handshake.
	certCheckInterval = 10 * time.Second
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

// reloadingCert holds a certificate loaded from disk and reloads it when either
// file's modification time changes. A pair caught mid-rotation fails to load and
// the previous one stays in service until the next check.
type reloadingCert struct {
	certFile, keyFile string

	mu        sync.Mutex
	cert      *tls.Certificate
	certMod   time.Time
	keyMod    time.Time
	checkedAt time.Time
}

func loadCert(certFile, keyFile string) (*reloadingCert, error) {
	if certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("%s is set, so %s and %s must be too", envHTTPSPort, envCertFile, envKeyFile)
	}
	c := &reloadingCert{certFile: certFile, keyFile: keyFile}
	if err := c.load(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *reloadingCert) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.checkedAt) >= certCheckInterval {
		if certMod, keyMod, err := c.modTimes(); err == nil && (!certMod.Equal(c.certMod) || !keyMod.Equal(c.keyMod)) {
			_ = c.load()
		}
		c.checkedAt = time.Now()
	}
	return c.cert, nil
}

func (c *reloadingCert) load() error {
	certMod, keyMod, err := c.modTimes()
	if err != nil {
		return err
	}
	pair, err := tls.LoadX509KeyPair(c.certFile, c.keyFile)
	if err != nil {
		return fmt.Errorf("load certificate %s and key %s: %w", c.certFile, c.keyFile, err)
	}
	c.cert, c.certMod, c.keyMod, c.checkedAt = &pair, certMod, keyMod, time.Now()
	return nil
}

func (c *reloadingCert) modTimes() (time.Time, time.Time, error) {
	certInfo, certErr := os.Stat(c.certFile)
	keyInfo, keyErr := os.Stat(c.keyFile)
	if err := errors.Join(certErr, keyErr); err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("stat certificate files: %w", err)
	}
	return certInfo.ModTime(), keyInfo.ModTime(), nil
}
