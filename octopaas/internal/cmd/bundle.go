package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
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
		if f.Name == manifestName {
			target = filepath.Join(root, keptManifest)
		}
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

// manifestName is the bundle's metadata entry, at the root of the archive.
const manifestName = "octo-bundle.json"

// keptManifest is where pull keeps the manifest in the folder it unpacks into:
// in .octo/, the folder's own metadata, where neither the runtime nor the editor
// reads it as one of the integration's resources.
var keptManifest = filepath.Join(".octo", manifestName)

// manifest is the bundle's metadata entry as pack writes it: which entries are
// config files and what kind each resource is. Pack writes a fresh one in the
// current format rather than editing the pulled one, carrying over only the name,
// the tag and each resource's kind; anything else an older format held is read
// from the folder instead.
type manifest struct {
	Version   int             `json:"version"`
	Name      string          `json:"name"`
	Tag       string          `json:"tag,omitempty"`
	Configs   []string        `json:"configs"`
	Resources []manifestEntry `json:"resources"`
}

type manifestEntry struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// pack zips the files under dir. Hidden files and folders (.git, .DS_Store,
// .env) are left out: they are never part of an integration, and .env may hold
// secrets. The exception is a hidden file the folder's manifest lists as a
// resource, which came from the platform and goes back to it.
//
// A pulled folder carries the manifest it was pulled with (in .octo/, or at its
// root when an older octopaas pulled it), and the platform takes a manifest as the
// whole truth. So it is rewritten to describe the folder as it
// is now: files added since the pull are listed, removed ones are not, and each
// resource keeps the kind the platform gave it.
func pack(dir string) ([]byte, int, error) {

	pulled, err := readManifest(dir)
	if err != nil {
		return nil, 0, err
	}
	listed := map[string]bool{}
	if pulled != nil {
		for _, r := range pulled.Resources {
			listed[r.Name] = true
		}
	}

	var files []string
	err = filepath.WalkDir(dir, func(file string, d fs.DirEntry, err error) error {

		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, file)
		if err != nil || rel == "." {
			return err
		}
		name := filepath.ToSlash(rel)
		if hiddenPath(name) && !listed[name] {
			//a hidden folder is still walked when the manifest lists something
			//inside it; everything else in it stays behind with it.
			if d.IsDir() && !listsUnder(listed, name) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		if pulled != nil && name == manifestName {
			return nil
		}
		files = append(files, name)
		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("pack %s: %w", dir, err)
	}
	if len(files) == 0 {
		return nil, 0, fmt.Errorf("%s has no files to push", dir)
	}

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	if pulled != nil {
		doc, err := json.MarshalIndent(describe(*pulled, files), "", "  ")
		if err != nil {
			return nil, 0, err
		}
		dst, err := w.Create(manifestName)
		if err != nil {
			return nil, 0, err
		}
		if _, err := dst.Write(append(doc, '\n')); err != nil {
			return nil, 0, err
		}
	}
	for _, name := range files {
		if err := addFile(w, dir, name); err != nil {
			return nil, 0, fmt.Errorf("pack %s: %w", dir, err)
		}
	}
	if err := w.Close(); err != nil {
		return nil, 0, err
	}
	if buf.Len() > maxBundle {
		return nil, len(files), fmt.Errorf("%s packs to %d bytes, more than the 8 MiB the API takes", dir, buf.Len())
	}
	return buf.Bytes(), len(files), nil
}

func addFile(w *zip.Writer, dir, name string) error {

	dst, err := w.Create(name)
	if err != nil {
		return err
	}
	src, err := os.Open(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	_, err = io.Copy(dst, src)
	return err
}

// readManifest reads the manifest pull left in dir, or nil when there is none.
func readManifest(dir string) (*manifest, error) {

	for _, name := range []string{keptManifest, manifestName} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var m manifest
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("%s in %s does not parse: %w", name, dir, err)
		}
		return &m, nil
	}
	return nil, nil
}

// hiddenPath is whether name, or any folder it is in, is hidden.
func hiddenPath(name string) bool {

	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

// listsUnder is whether the manifest lists a resource somewhere inside dir.
func listsUnder(listed map[string]bool, dir string) bool {

	for name := range listed {
		if strings.HasPrefix(name, dir+"/") {
			return true
		}
	}
	return false
}

// describe is the manifest for files, by the platform's own rule for a folder: a
// config file is a .yaml or .yml directly in it that is not a _test suite, and
// every other file is a resource. A resource the pulled manifest named keeps its
// kind; a new one is an env file when its name starts with .env, as the platform
// guesses for a bundle without a manifest.
func describe(pulled manifest, files []string) manifest {

	kinds := map[string]string{}
	for _, r := range pulled.Resources {
		kinds[r.Name] = r.Kind
	}
	out := manifest{Version: 2, Name: pulled.Name, Tag: pulled.Tag, Configs: []string{}, Resources: []manifestEntry{}}
	for _, name := range files {
		if isConfig(name) {
			out.Configs = append(out.Configs, name)
			continue
		}
		kind, ok := kinds[name]
		if !ok {
			kind = "template"
			if strings.HasPrefix(path.Base(name), ".env") {
				kind = "env"
			}
		}
		out.Resources = append(out.Resources, manifestEntry{Name: name, Kind: kind})
	}
	return out
}

func isConfig(name string) bool {

	if strings.Contains(name, "/") {
		return false
	}
	ext := path.Ext(name)
	switch strings.ToLower(ext) {
	case ".yaml", ".yml":
		return !strings.HasSuffix(strings.TrimSuffix(name, ext), "_test")
	}
	return false
}

// readBundle is a folder packed, or a .zip read as it is.
func readBundle(target string) ([]byte, int, error) {

	info, err := os.Stat(target)
	if err != nil {
		return nil, 0, err
	}
	if info.IsDir() {
		return pack(target)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return nil, 0, err
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, 0, fmt.Errorf("%s is neither a folder nor a zip", target)
	}
	return data, len(r.File), nil
}

// emptyOrMissing says whether dir can be pulled into without clobbering
// anything.
func emptyOrMissing(dir string) bool {

	entries, err := os.ReadDir(dir)
	return err != nil || len(entries) == 0
}
