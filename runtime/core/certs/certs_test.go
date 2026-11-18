package certs

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core/certs/certstest"
)

func TestFromPEMRejectsMismatchedPair(t *testing.T) {
	certA, _ := certstest.SelfSigned(t, "a.test")
	_, keyB := certstest.SelfSigned(t, "b.test")
	if _, err := FromPEM(certA, keyB); err == nil {
		t.Fatal("expected an error for a key that does not match the certificate")
	}
}

func TestFromFilesFailsOnMissingFiles(t *testing.T) {
	dir := t.TempDir()
	if _, err := FromFiles(filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")); err == nil {
		t.Fatal("expected an error for missing files")
	}
}

func TestFromFilesPicksUpRotation(t *testing.T) {
	certFile, keyFile := pairPaths(t)
	writePair(t, certFile, keyFile, "old.test", time.Now().Add(-time.Hour))
	f, clock := fileSource(t, certFile, keyFile)
	first := leaf(t, f)

	writePair(t, certFile, keyFile, "new.test", time.Now())
	if got := leaf(t, f); !bytes.Equal(got, first) {
		t.Fatal("reloaded inside the check interval")
	}

	*clock = clock.Add(reloadCheckInterval)
	if got := leaf(t, f); bytes.Equal(got, first) {
		t.Fatal("did not reload a rotated pair after the check interval")
	}
}

func TestFromFilesKeepsLastGoodPairOnBrokenRotation(t *testing.T) {
	certFile, keyFile := pairPaths(t)
	writePair(t, certFile, keyFile, "good.test", time.Now().Add(-time.Hour))
	f, clock := fileSource(t, certFile, keyFile)
	first := leaf(t, f)

	if err := os.WriteFile(certFile, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(reloadCheckInterval)
	if got := leaf(t, f); !bytes.Equal(got, first) {
		t.Fatal("a broken rotation replaced the serving pair")
	}
}

func TestFromEnv(t *testing.T) {
	certFile, keyFile := pairPaths(t)
	writePair(t, certFile, keyFile, "env.test", time.Now())
	cases := []struct {
		name       string
		cert, key  string
		wantSource bool
		wantErr    bool
	}{
		{name: "unset"},
		{name: "both", cert: certFile, key: keyFile, wantSource: true},
		{name: "cert only", cert: certFile, wantErr: true},
		{name: "key only", key: keyFile, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvCertFile, tc.cert)
			t.Setenv(EnvKeyFile, tc.key)
			src, err := FromEnv()
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if (src != nil) != tc.wantSource {
				t.Fatalf("source present = %v, want %v", src != nil, tc.wantSource)
			}
		})
	}
}

func pairPaths(t *testing.T) (string, string) {
	dir := t.TempDir()
	return filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
}

func fileSource(t *testing.T, certFile, keyFile string) (*files, *time.Time) {
	t.Helper()
	f, err := newFiles(certFile, keyFile)
	if err != nil {
		t.Fatalf("newFiles: %v", err)
	}
	clock := time.Now()
	f.now = func() time.Time { return clock }
	f.checkedAt = clock
	return f, &clock
}

func writePair(t *testing.T, certFile, keyFile, host string, mod time.Time) {
	t.Helper()
	certPEM, keyPEM := certstest.SelfSigned(t, host)
	for path, data := range map[string][]byte{certFile: certPEM, keyFile: keyPEM} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
}

func leaf(t *testing.T, f *files) []byte {
	t.Helper()
	return f.current().Certificate[0]
}
