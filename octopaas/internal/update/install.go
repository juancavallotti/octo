package update

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// maxArchive bounds what a download may be: the release archives are a few
// megabytes, and nothing honest is a hundred.
const maxArchive = 100 << 20

// Asset is the archive name a release carries for goos/goarch.
func Asset(goos, goarch string) string {

	if goos == "windows" {
		return "octopaas_" + goos + "_" + goarch + ".zip"
	}
	return "octopaas_" + goos + "_" + goarch + ".tar.gz"
}

// Progress is told how many bytes of the archive have arrived, of how many.
type Progress func(done, total int64)

// Install downloads the release tag for goos/goarch, checks it against the
// release's checksums.txt, and puts the binary inside at exe in place of the
// one there. The swap is a rename, so exe is never half-written: a failure
// anywhere leaves the old binary exactly as it was.
func (s Source) Install(ctx context.Context, tag, goos, goarch, exe string, progress Progress) error {

	asset := Asset(goos, goarch)
	sums, err := s.fetch(ctx, tag, "checksums.txt", nil)
	if err != nil {
		return err
	}
	want, err := checksumFor(sums, asset)
	if err != nil {
		return fmt.Errorf("release %s carries no %s; it may predate octopaas, or not be built for this machine", tag, asset)
	}

	archive, err := s.fetch(ctx, tag, asset, progress)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(archive)
	if hex.EncodeToString(sum[:]) != want {
		return fmt.Errorf("%s does not match its checksum in %s's checksums.txt; refusing to install it", asset, tag)
	}

	binary, err := extract(archive, asset)
	if err != nil {
		return err
	}
	return replace(exe, binary, goos)
}

func (s Source) fetch(ctx context.Context, tag, name string, progress Progress) ([]byte, error) {

	url := s.Download + "/" + tag + "/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s carries no %s; it may predate octopaas, or not be built for this machine", tag, name)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: the server answered %d", name, resp.StatusCode)
	}

	src := io.LimitReader(resp.Body, maxArchive)
	if progress != nil {
		src = &counting{r: src, total: resp.ContentLength, progress: progress}
	}
	data, err := io.ReadAll(src)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", name, err)
	}
	return data, nil
}

type counting struct {
	r        io.Reader
	done     int64
	total    int64
	progress Progress
}

func (c *counting) Read(p []byte) (int, error) {

	n, err := c.r.Read(p)
	c.done += int64(n)
	c.progress(c.done, c.total)
	return n, err
}

// checksumFor finds asset's line in a checksums.txt: "<sha256>  <name>".
func checksumFor(sums []byte, asset string) (string, error) {

	scanner := bufio.NewScanner(bytes.NewReader(sums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == asset {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("checksums.txt has no line for %s", asset)
}

// extract pulls the octopaas binary out of a release archive.
func extract(archive []byte, asset string) ([]byte, error) {

	if strings.HasSuffix(asset, ".zip") {
		r, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, fmt.Errorf("%s is not a zip: %w", asset, err)
		}
		for _, f := range r.File {
			if path.Base(f.Name) == "octopaas.exe" {
				src, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer func() { _ = src.Close() }()
				return io.ReadAll(io.LimitReader(src, maxArchive))
			}
		}
		return nil, fmt.Errorf("%s holds no octopaas.exe", asset)
	}

	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("%s is not a gzip: %w", asset, err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s holds no octopaas binary", asset)
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", asset, err)
		}
		if h.Typeflag == tar.TypeReg && path.Base(h.Name) == "octopaas" {
			return io.ReadAll(io.LimitReader(tr, maxArchive))
		}
	}
}

// replace writes binary beside exe and renames it over. Windows will not rename
// over a running executable, but it will rename the running one aside, so there
// the old binary moves to exe.old first.
func replace(exe string, binary []byte, goos string) error {

	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".octopaas-upgrade-*")
	if err != nil {
		return fmt.Errorf("write beside %s: %w", exe, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(binary); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}

	if goos == "windows" {
		old := exe + ".old"
		_ = os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return fmt.Errorf("move the running octopaas aside: %w", err)
		}
	}
	if err := os.Rename(tmp.Name(), exe); err != nil {
		return fmt.Errorf("put the new octopaas in place: %w", err)
	}
	return nil
}

// Writable says whether the folder holding exe can take the new binary.
func Writable(exe string) bool {

	f, err := os.CreateTemp(filepath.Dir(exe), ".octopaas-probe-*")
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	return true
}

// Executable is the path of the running binary, symlinks resolved, so an
// upgrade replaces the file and not the link to it.
func Executable() (string, error) {

	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}
