package bundle

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// maxSlugLen bounds the slug derived from an integration name, so a pathological
// name cannot produce an unusable filename.
const maxSlugLen = 64

// Write renders a bundle as a zip archive: the manifest, each config file at its
// own path, then one entry per resource at its own name.
//
// Paths and names are written verbatim because they are already confined —
// relative, no empty segments, no "..", enforced when the file is stored. A name
// that escaped would escape on extraction too, so it is rejected here rather than
// sanitized into something the config no longer refers to.
func Write(b Bundle) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	doc, err := json.MarshalIndent(manifestOf(b), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("bundle: render manifest: %w", err)
	}
	if err := writeEntry(zw, manifestName, string(doc)+"\n"); err != nil {
		return nil, err
	}
	for _, c := range b.Configs {
		if err := checkConfigPath(c.Path); err != nil {
			return nil, err
		}
		if err := writeEntry(zw, c.Path, c.Content); err != nil {
			return nil, err
		}
	}
	for _, r := range b.Resources {
		if err := safeEntryName(r.Name); err != nil {
			return nil, err
		}
		// The manifest's name is reserved. A resource may legally be called this —
		// nothing in the resource store forbids it — and writing it would produce an
		// archive whose manifest entry is a resource: readable as a zip, unreadable
		// as a bundle. Refusing here says so, rather than failing on import.
		if r.Name == manifestName {
			return nil, fmt.Errorf("%w: resource %q uses the reserved manifest name", ErrInvalid, r.Name)
		}
		if err := writeEntry(zw, r.Name, r.Content); err != nil {
			return nil, err
		}
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("bundle: finish archive: %w", err)
	}
	return buf.Bytes(), nil
}

// writeEntry adds one stored file to the archive.
func writeEntry(zw *zip.Writer, name, content string) error {
	w, err := zw.Create(name)
	if err != nil {
		return fmt.Errorf("bundle: create entry %q: %w", name, err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		return fmt.Errorf("bundle: write entry %q: %w", name, err)
	}
	return nil
}

// manifestOf describes a bundle's entries.
func manifestOf(b Bundle) manifest {
	configs := make([]string, 0, len(b.Configs))
	for _, c := range b.Configs {
		configs = append(configs, c.Path)
	}
	entries := make([]manifestEntry, 0, len(b.Resources))
	for _, r := range b.Resources {
		entries = append(entries, manifestEntry{Name: r.Name, Kind: r.Kind})
	}
	return manifest{
		Version:   manifestVersion,
		Name:      b.Name,
		Tag:       b.Tag,
		Configs:   configs,
		Resources: entries,
	}
}

// Slug turns a display name into a filename stem: lowercase, non-alphanumerics
// collapsed to '-', trimmed and length-bounded.
func Slug(name string) string {
	var sb strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			sb.WriteRune(r)
			lastDash = false
		case !lastDash:
			sb.WriteByte('-')
			lastDash = true
		}
	}
	s := strings.Trim(sb.String(), "-")
	if len(s) > maxSlugLen {
		s = strings.Trim(s[:maxSlugLen], "-")
	}
	if s == "" {
		return fallbackSlug
	}
	return s
}
