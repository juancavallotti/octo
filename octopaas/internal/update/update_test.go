package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tarball(t *testing.T, name string, content []byte) []byte {

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "LICENSE", Mode: 0o644, Size: 3, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("MIT"))
	_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(content)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// releases is a GitHub stand-in with one release, v0.12.0, whose checksum for
// the linux/amd64 archive can be made to lie.
func releases(t *testing.T, archive []byte, lie bool) Source {

	sum := sha256.Sum256(archive)
	digest := hex.EncodeToString(sum[:])
	if lie {
		digest = strings.Repeat("0", 64)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/releases/latest":
			_, _ = fmt.Fprint(w, `{"tag_name":"v0.12.0"}`)
		case "/dl/v0.12.0/checksums.txt":
			_, _ = fmt.Fprintf(w, "%s  octopaas_linux_amd64.tar.gz\n%s  octo_linux_amd64.tar.gz\n", digest, strings.Repeat("1", 64))
		case "/dl/v0.12.0/octopaas_linux_amd64.tar.gz":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return Source{API: srv.URL + "/api", Download: srv.URL + "/dl", HTTP: srv.Client()}
}

func TestNewer(t *testing.T) {

	cases := []struct {
		current, release string
		want             bool
	}{
		{"0.11.10", "v0.12.0", true},
		{"0.11.10", "v0.11.10", false},
		{"0.11.10", "v0.11.9", false},
		{"0.9.0", "v0.10.0", true}, //compared as numbers, not text
		{"0.11.10", "nightly", false},
	}
	for _, c := range cases {
		if got := Newer(c.current, c.release); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.current, c.release, got, c.want)
		}
	}
}

// The answer is cached for a day, so the notice costs GitHub one call a day.
func TestTheLatestReleaseIsCachedForADay(t *testing.T) {

	home := t.TempDir()
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	if _, fresh := Cached(home, now); fresh {
		t.Fatal("nothing should be cached before the first check")
	}
	if _, err := Refresh(context.Background(), releases(t, nil, false), home, now); err != nil {
		t.Fatal(err)
	}
	if got, fresh := Cached(home, now.Add(23*time.Hour)); !fresh || got.Latest != "v0.12.0" {
		t.Errorf("a check from 23h ago should still be fresh, got %+v fresh=%v", got, fresh)
	}
	if _, fresh := Cached(home, now.Add(25*time.Hour)); fresh {
		t.Error("a check from 25h ago should be stale")
	}
}

// The whole upgrade: the archive is checked against checksums.txt and the
// binary inside it takes the old one's place.
func TestInstallReplacesTheBinary(t *testing.T) {

	exe := filepath.Join(t.TempDir(), "octopaas")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := releases(t, tarball(t, "octopaas", []byte("new binary")), false)

	var moved int64
	if err := src.Install(context.Background(), "v0.12.0", "linux", "amd64", exe, func(done, _ int64) { moved = done }); err != nil {
		t.Fatalf("Install: %v", err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "new binary" {
		t.Errorf("exe holds %q, want the new binary", got)
	}
	if info, _ := os.Stat(exe); info.Mode().Perm()&0o100 == 0 {
		t.Errorf("the new binary should be executable, is %v", info.Mode())
	}
	if moved == 0 {
		t.Error("progress should hear about the download")
	}
}

// An archive that does not match its published checksum is never installed,
// and the binary already there is left exactly as it was.
func TestAChecksumMismatchLeavesTheOldBinary(t *testing.T) {

	exe := filepath.Join(t.TempDir(), "octopaas")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := releases(t, tarball(t, "octopaas", []byte("tampered")), true)

	err := src.Install(context.Background(), "v0.12.0", "linux", "amd64", exe, nil)
	if err == nil || !strings.Contains(err.Error(), "does not match its checksum") {
		t.Fatalf("a mismatched archive should be refused, got %v", err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "old" {
		t.Errorf("the old binary should be untouched, holds %q", got)
	}
	entries, _ := os.ReadDir(filepath.Dir(exe))
	if len(entries) != 1 {
		t.Errorf("nothing should be left beside the binary, found %d entries", len(entries))
	}
}

// A release built before octopaas shipped, or for a machine it is not built
// for, says so rather than failing obscurely.
func TestAMissingArchiveIsExplained(t *testing.T) {

	src := releases(t, nil, false)
	err := src.Install(context.Background(), "v0.12.0", "plan9", "386", filepath.Join(t.TempDir(), "octopaas"), nil)
	if err == nil || !strings.Contains(err.Error(), "carries no octopaas_plan9_386.tar.gz") {
		t.Fatalf("got %v", err)
	}
}
