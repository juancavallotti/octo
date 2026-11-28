// Package certs supplies server certificates to TLS listeners. A Source hands out
// the certificate for each handshake, so a source backed by files can swap in a
// renewed pair without the listener being rebuilt.
package certs

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

const (
	// reloadCheckInterval bounds how often a file source rereads its files.
	// Handshakes inside the interval reuse the loaded pair, so a busy listener
	// does not read the disk on every connection.
	reloadCheckInterval = 10 * time.Second

	// EnvCertFile names the certificate file a runtime serves TLS with, as read
	// by FromEnv.
	EnvCertFile = "OCTO_TLS_CERT_FILE"
	// EnvKeyFile names the private key file paired with EnvCertFile.
	EnvKeyFile = "OCTO_TLS_KEY_FILE"
)

// Source returns the certificate to present for a TLS handshake.
type Source struct {
	get func() *tls.Certificate
}

// GetCertificate has the shape of tls.Config.GetCertificate so it can be plugged
// in directly.
func (s *Source) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return s.get(), nil
}

// FromPEM parses a PEM-encoded certificate chain and private key into a Source.
func FromPEM(certPEM, keyPEM []byte) (*Source, error) {
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse certificate and key: %w", err)
	}
	return &Source{get: func() *tls.Certificate { return &pair }}, nil
}

// FromEnv returns a file Source for the pair named by EnvCertFile and EnvKeyFile,
// or nil and no error when neither is set. Setting only one is an error.
func FromEnv() (*Source, error) {
	certFile, keyFile := os.Getenv(EnvCertFile), os.Getenv(EnvKeyFile)
	switch {
	case certFile == "" && keyFile == "":
		return nil, nil
	case certFile == "" || keyFile == "":
		return nil, fmt.Errorf("%s and %s must be set together", EnvCertFile, EnvKeyFile)
	}
	return FromFiles(certFile, keyFile)
}

// files holds a certificate and key loaded from disk. It loads the pair up front
// and, at each check, rereads both files and adopts them when their bytes differ
// from the pair in service. Comparing contents rather than modification times
// catches a renewal written by a sync that preserves timestamps.
type files struct {
	certFile, keyFile string
	now               func() time.Time

	mu        sync.Mutex
	cert      *tls.Certificate
	certPEM   []byte
	keyPEM    []byte
	checkedAt time.Time
}

// FromFiles loads a certificate and key from disk into a Source that picks up a
// renewed pair on its own. It fails when the files cannot be read or do not form
// a valid pair; a later failed reload keeps serving the last good pair.
func FromFiles(certFile, keyFile string) (*Source, error) {
	f, err := newFiles(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	return &Source{get: f.current}, nil
}

func newFiles(certFile, keyFile string) (*files, error) {
	f := &files{certFile: certFile, keyFile: keyFile, now: time.Now}
	if err := f.load(); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *files) current() *tls.Certificate {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.now().Sub(f.checkedAt) >= reloadCheckInterval {
		f.checkedAt = f.now()
		// A pair caught mid-rotation (new cert, old key) fails to parse; the
		// previous pair stays in service and the next check tries again.
		_ = f.loadLocked()
	}
	return f.cert
}

func (f *files) load() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checkedAt = f.now()
	return f.loadLocked()
}

// loadLocked reads both files and, when either differs from the pair in service,
// parses exactly the bytes it read and swaps them in. Parsing the bytes already
// in hand, rather than reopening the files, means the pair adopted is the pair
// that was validated.
func (f *files) loadLocked() error {
	certPEM, certErr := os.ReadFile(f.certFile)
	keyPEM, keyErr := os.ReadFile(f.keyFile)
	if err := errors.Join(certErr, keyErr); err != nil {
		return fmt.Errorf("read certificate files: %w", err)
	}
	if f.cert != nil && bytes.Equal(certPEM, f.certPEM) && bytes.Equal(keyPEM, f.keyPEM) {
		return nil
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("load certificate %s and key %s: %w", f.certFile, f.keyFile, err)
	}
	f.cert, f.certPEM, f.keyPEM = &pair, certPEM, keyPEM
	return nil
}
