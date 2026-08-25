package bundle

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
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
// A manifest is authoritative when present: it names the definition entry and
// states each resource's kind, so a bundle this orchestrator wrote round-trips
// exactly. Without one — a hand-made zip, or a directory someone zipped up — the
// layout is read instead: the single root-level YAML is the definition, every
// other file is a resource, and its kind is guessed from its name. Name is empty
// for a manifest-less archive; the caller supplies one.
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
// describes an integration that would import with a resource silently missing.
func fromManifest(doc string, files map[string]string) (Bundle, error) {
	var m manifest
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		return Bundle{}, fmt.Errorf("%w: %s does not parse", ErrInvalid, manifestName)
	}
	if m.Version != manifestVersion {
		return Bundle{}, fmt.Errorf("%w: unsupported bundle version %d (this install writes %d)",
			ErrInvalid, m.Version, manifestVersion)
	}
	definition, ok := files[m.Definition]
	if !ok {
		return Bundle{}, fmt.Errorf("%w: the manifest names %q as the definition, which the archive does not contain",
			ErrInvalid, m.Definition)
	}

	out := Bundle{Name: strings.TrimSpace(m.Name), Tag: m.Tag, Definition: definition}
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

// fromLayout reads an archive that carries no manifest, inferring the roles from
// the file layout: the one root-level YAML is the definition and everything else
// is a resource. Zero or several candidates is ambiguous, and guessing which
// document is the integration is exactly the kind of guess that imports the wrong
// thing without saying so.
func fromLayout(files map[string]string) (Bundle, error) {
	var candidates []string
	for name := range files {
		if isRootYAML(name) {
			candidates = append(candidates, name)
		}
	}
	sort.Strings(candidates)
	if len(candidates) != 1 {
		return Bundle{}, fmt.Errorf(
			"%w: expected exactly one .yaml at the archive root to be the definition, found %d",
			ErrInvalid, len(candidates))
	}
	definitionName := candidates[0]

	out := Bundle{Definition: files[definitionName]}
	names := make([]string, 0, len(files))
	for name := range files {
		if name != definitionName {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		out.Resources = append(out.Resources, File{Kind: guessKind(name), Name: name, Content: files[name]})
	}
	return out, nil
}

// isRootYAML reports whether an entry is a YAML document directly at the archive
// root. Nested YAML is a resource (a template can be YAML), so only the root is
// considered.
func isRootYAML(name string) bool {
	if strings.Contains(name, "/") {
		return false
	}
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml")
}
