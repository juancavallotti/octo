package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHTTPSFromEnvUnsetIsNil(t *testing.T) {
	t.Setenv(envHTTPSPort, "")
	srv, err := httpsFromEnv(&http.Server{})
	if err != nil || srv != nil {
		t.Fatalf("got %v, %v; want no server and no error", srv, err)
	}
}

func TestHTTPSFromEnvNeedsCertificate(t *testing.T) {
	dir := t.TempDir()
	cases := map[string][2]string{
		"unset":   {"", ""},
		"missing": {filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv(envHTTPSPort, "8443")
			t.Setenv(envCertFile, files[0])
			t.Setenv(envKeyFile, files[1])
			if _, err := httpsFromEnv(&http.Server{}); err == nil {
				t.Fatal("expected an error without a usable certificate")
			}
		})
	}
}

func TestReloadingCertKeepsLastGoodPairUntilRotationCompletes(t *testing.T) {
	a, b := newPair(t, "a"), newPair(t, "b")
	c, clock, files := servePair(t, a)

	writeFile(t, files.cert, b.cert)
	clock.advance(certCheckInterval)
	assertServes(t, c, a, "a certificate that does not match its key")

	writeFile(t, files.key, b.key)
	clock.advance(certCheckInterval)
	assertServes(t, c, b, "the completed rotation")
}

func TestReloadingCertAdoptsReplacementWithUnchangedModTimes(t *testing.T) {
	a, b := newPair(t, "a"), newPair(t, "b")
	c, clock, files := servePair(t, a)
	certInfo, keyInfo := stat(t, files.cert), stat(t, files.key)

	writeFile(t, files.cert, b.cert)
	writeFile(t, files.key, b.key)
	if err := os.Chtimes(files.cert, certInfo.ModTime(), certInfo.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(files.key, keyInfo.ModTime(), keyInfo.ModTime()); err != nil {
		t.Fatal(err)
	}
	clock.advance(certCheckInterval)
	assertServes(t, c, b, "a replacement that kept the old modification times")
}

type pemPair struct{ cert, key []byte }

type certFiles struct{ cert, key string }

type fakeClock struct{ t time.Time }

func (f *fakeClock) now() time.Time { return f.t }

func (f *fakeClock) advance(d time.Duration) { f.t = f.t.Add(d) }

func servePair(t *testing.T, p pemPair) (*reloadingCert, *fakeClock, certFiles) {
	t.Helper()
	dir := t.TempDir()
	files := certFiles{cert: filepath.Join(dir, "tls.crt"), key: filepath.Join(dir, "tls.key")}
	writeFile(t, files.cert, p.cert)
	writeFile(t, files.key, p.key)
	c, err := loadCert(files.cert, files.key)
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{t: c.checkedAt}
	c.now = clock.now
	assertServes(t, c, p, "the initial pair")
	return c, clock, files
}

func assertServes(t *testing.T, c *reloadingCert, want pemPair, after string) {
	t.Helper()
	got, err := c.get(nil)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(want.cert)
	if !bytes.Equal(got.Certificate[0], block.Bytes) {
		t.Fatalf("after %s, the listener serves the wrong certificate", after)
	}
}

func newPair(t *testing.T, name string) pemPair {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pemPair{
		cert: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		key:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
	}
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func stat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}
