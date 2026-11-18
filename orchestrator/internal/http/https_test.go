package httpx

import (
	"net/http"
	"path/filepath"
	"testing"
)

func TestHTTPSFromEnvUnsetIsNil(t *testing.T) {
	t.Setenv(envHTTPSPort, "")
	srv, err := HTTPSFromEnv(&http.Server{})
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
			if _, err := HTTPSFromEnv(&http.Server{}); err == nil {
				t.Fatal("expected an error without a usable certificate")
			}
		})
	}
}
