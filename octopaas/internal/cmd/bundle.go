package cmd

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// A bundle is an integration as a zip: its definition and every resource, the
// same shape a local folder is opened from. Pull unpacks one into a folder and
// push packs a folder back up, so editing an integration is editing files.

// maxBundle is the most the API takes in one bundle.
const maxBundle = 8 << 20

// unpack writes a bundle's files under dir. It refuses any entry that would land
// outside dir — a name with "..", or an absolute one — because a bundle is data
// from a server and must not be able to write wherever it likes.
func unpack(data []byte, dir string) (int, error) {

	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return 0, fmt.Errorf("the bundle is not a zip: %w", err)
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, f := range r.File {
		target := filepath.Join(root, filepath.FromSlash(f.Name))
		if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
			return count, fmt.Errorf("the bundle has an entry outside its folder (%q); refusing to unpack it", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return count, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return count, err
		}
		if err := extract(f, target); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func extract(f *zip.File, target string) error {

	src, err := f.Open()
	if err != nil {
		return fmt.Errorf("read %s from the bundle: %w", f.Name, err)
	}
	defer func() { _ = src.Close() }()
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	//a bundle is at most maxBundle compressed; we bound what one entry may
	//inflate to as well, so a zip bomb fills a file and not the disk.
	if _, err := io.Copy(dst, io.LimitReader(src, 16*maxBundle)); err != nil {
		_ = dst.Close()
		return fmt.Errorf("write %s: %w", target, err)
	}
	return dst.Close()
}

// pack zips the files under dir. Hidden files and folders (.git, .DS_Store,
// .env) are left out: they are never part of an integration, and .env may hold
// secrets.
func pack(dir string) ([]byte, int, error) {

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	count := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {

		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil || rel == "." {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}

		dst, err := w.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = src.Close() }()
		if _, err := io.Copy(dst, src); err != nil {
			return err
		}
		count++
		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("pack %s: %w", dir, err)
	}
	if err := w.Close(); err != nil {
		return nil, 0, err
	}
	if count == 0 {
		return nil, 0, fmt.Errorf("%s has no files to push", dir)
	}
	if buf.Len() > maxBundle {
		return nil, count, fmt.Errorf("%s packs to %d bytes, more than the 8 MiB the API takes", dir, buf.Len())
	}
	return buf.Bytes(), count, nil
}

// readBundle is a folder packed, or a .zip read as it is.
func readBundle(path string) ([]byte, int, error) {

	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, err
	}
	if info.IsDir() {
		return pack(path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, 0, fmt.Errorf("%s is neither a folder nor a zip", path)
	}
	return data, len(r.File), nil
}

// emptyOrMissing says whether dir can be pulled into without clobbering
// anything.
func emptyOrMissing(dir string) bool {

	entries, err := os.ReadDir(dir)
	return err != nil || len(entries) == 0
}
