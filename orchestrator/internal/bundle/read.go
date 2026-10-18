package bundle

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/juancavallotti/octo/orchestrator/internal/projectfile"
)

const (
	// maxEntries bounds how many files an archive may contain. An integration with
	// hundreds of resources is already unusual; thousands is an attack.
	maxEntries = 512
	// maxTotalBytes bounds the *uncompressed* size of everything read out of an
	// archive. The compressed upload is capped separately by the handler; this is
	// what stops a small upload from expanding without bound.
	maxTotalBytes = 32 << 20 // 32 MiB
)

// Read parses a zip archive into a Bundle.
//
// A manifest is authoritative when present: it lists the config entries and
// states each resource's kind, so a bundle this orchestrator wrote round-trips
// exactly. Without one — a hand-made zip, or a directory someone zipped up — the
// layout is read instead, the way the runtime reads a directory: every root-level,
// non-test YAML is a config file, every other file is a resource, and its kind is
// guessed from its name. Name is empty for a manifest-less archive; the caller
// supplies one.
func Read(data []byte) (Bundle, error) {
	files, err := readEntries(data)
	if err != nil {
		return Bundle{}, err
	}
	if doc, ok := files[manifestName]; ok {
		return fromManifest(doc, files)
	}
	return fromLayout(files)
}

// readEntries reads every file entry into memory, keyed by name, enforcing the
// entry-count, total-size and containment limits. Directory entries carry no
// content and are skipped; the names they imply are recreated on extraction.
func readEntries(data []byte) (map[string]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: not a readable zip archive", ErrInvalid)
	}
	if len(zr.File) > maxEntries {
		return nil, fmt.Errorf("%w: more than %d entries", ErrTooLarge, maxEntries)
	}

	files := make(map[string]string, len(zr.File))
	budget := int64(maxTotalBytes)
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "/") {
			continue
		}
		if err := safeEntryName(f.Name); err != nil {
			return nil, err
		}
		// A zip may carry two entries under one name; keying by name would silently
		// keep the last. That is how a hand-made archive would smuggle a resource
		// onto the manifest's name, and how a bundle's own file would be shadowed.
		if _, dup := files[f.Name]; dup {
			return nil, fmt.Errorf("%w: entry %q appears more than once", ErrInvalid, f.Name)
		}
		content, read, err := readEntry(f, budget)
		if err != nil {
			return nil, err
		}
		budget -= read
		files[f.Name] = content
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%w: the archive is empty", ErrInvalid)
	}
	return files, nil
}

// readEntry reads one entry, refusing to read more than the remaining budget. It
// asks for one byte past the budget so an entry that fills it exactly is told
// apart from one that would have overrun it.
func readEntry(f *zip.File, budget int64) (string, int64, error) {
	rc, err := f.Open()
	if err != nil {
		return "", 0, fmt.Errorf("%w: cannot read entry %q", ErrInvalid, f.Name)
	}
	defer func() { _ = rc.Close() }()

	content, err := io.ReadAll(io.LimitReader(rc, budget+1))
	if err != nil {
		return "", 0, fmt.Errorf("%w: cannot read entry %q", ErrInvalid, f.Name)
	}
	if int64(len(content)) > budget {
		return "", 0, fmt.Errorf("%w: contents expand past %d bytes", ErrTooLarge, maxTotalBytes)
	}
	return string(content), int64(len(content)), nil
}

// safeEntryName rejects an entry that would escape the directory it is extracted
// into, or that could not be stored as a resource name. It mirrors the resource
// service's own name rule, because an entry that passes here is about to become a
// resource name there.
func safeEntryName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("%w: an entry has no name", ErrInvalid)
	case strings.ContainsRune(name, '\\'):
		return fmt.Errorf("%w: entry %q contains a backslash", ErrInvalid, name)
	case strings.HasPrefix(name, "/"):
		return fmt.Errorf("%w: entry %q is an absolute path", ErrInvalid, name)
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("%w: entry %q is not a contained relative path", ErrInvalid, name)
		}
	}
	return nil
}

// fromManifest builds the bundle the manifest describes. Every entry it names
// must be present: a manifest that promises a file the archive does not carry
// describes an integration that would import with a file silently missing.
func fromManifest(doc string, files map[string]string) (Bundle, error) {
	var m manifest
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		return Bundle{}, fmt.Errorf("%w: %s does not parse", ErrInvalid, manifestName)
	}
	configs, err := manifestConfigs(m)
	if err != nil {
		return Bundle{}, err
	}

	out := Bundle{Name: strings.TrimSpace(m.Name), Tag: m.Tag}
	for _, p := range configs {
		content, ok := files[p]
		if !ok {
			return Bundle{}, fmt.Errorf("%w: the manifest lists config file %q, which the archive does not contain",
				ErrInvalid, p)
		}
		if err := checkConfigPath(p); err != nil {
			return Bundle{}, err
		}
		out.Configs = append(out.Configs, projectfile.File{Path: p, Content: content})
	}
	for _, e := range m.Resources {
		content, ok := files[e.Name]
		if !ok {
			return Bundle{}, fmt.Errorf("%w: the manifest lists resource %q, which the archive does not contain",
				ErrInvalid, e.Name)
		}
		if e.Kind != kindEnv && e.Kind != kindTemplate {
			return Bundle{}, fmt.Errorf("%w: resource %q has unknown kind %q", ErrInvalid, e.Name, e.Kind)
		}
		out.Resources = append(out.Resources, File{Kind: e.Kind, Name: e.Name, Content: content})
	}
	return out, nil
}

// manifestConfigs returns the config entries a manifest names, in the form its
// version writes them.
func manifestConfigs(m manifest) ([]string, error) {
	switch m.Version {
	case manifestVersion:
		return m.Configs, nil
	case singleDefinitionVersion:
		return []string{m.Definition}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported bundle version %d (this install writes %d)",
			ErrInvalid, m.Version, manifestVersion)
	}
}

// fromLayout reads an archive that carries no manifest, inferring the roles from
// the file layout the way the runtime loads a directory: each root-level, non-test
// YAML is a config file and everything else is a resource. An archive with no
// config file at all is refused, since importing one would produce an integration
// that does nothing and give no hint why.
func fromLayout(files map[string]string) (Bundle, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	var out Bundle
	for _, name := range names {
		if projectfile.Classify(name) == projectfile.RoleConfig {
			out.Configs = append(out.Configs, projectfile.File{Path: name, Content: files[name]})
			continue
		}
		out.Resources = append(out.Resources, File{Kind: guessKind(name), Name: name, Content: files[name]})
	}
	if len(out.Configs) == 0 {
		return Bundle{}, fmt.Errorf("%w: expected at least one .yaml config file at the archive root", ErrInvalid)
	}
	return out, nil
}

// checkConfigPath rejects a config entry that would escape on extraction or that
// the runtime would not load as config.
func checkConfigPath(p string) error {
	if err := safeEntryName(p); err != nil {
		return err
	}
	if role := projectfile.Classify(p); role != projectfile.RoleConfig {
		return fmt.Errorf("%w: %q is listed as a config file but would be a %s file", ErrInvalid, p, role)
	}
	return nil
}
