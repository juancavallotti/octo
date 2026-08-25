package bundle

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// maxSlugLen bounds the slug derived from an integration name, so a pathological
	// name cannot produce an unusable filename.
	maxSlugLen = 64
	// collisionLimit bounds the search for a free definition entry name when the
	// obvious one is taken by a resource. Far more attempts than a real integration
	// could need; it exists so the loop cannot run forever.
	collisionLimit = 100
)

// Write renders a bundle as a zip archive: the manifest, the definition, then one
// entry per resource at its own name.
//
// Resource names are written verbatim because they are already confined —
// relative, no empty segments, no "..", enforced when the resource is stored. A
// name that escaped would escape on extraction too, so it is rejected here rather
// than sanitized into something the definition no longer refers to.
func Write(b Bundle) ([]byte, error) {
	definitionName, err := definitionEntryName(b)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	entries := make([]manifestEntry, 0, len(b.Resources))
	for _, r := range b.Resources {
		entries = append(entries, manifestEntry{Name: r.Name, Kind: r.Kind})
	}
	doc, err := json.MarshalIndent(manifest{
		Version:    manifestVersion,
		Name:       b.Name,
		Tag:        b.Tag,
		Definition: definitionName,
		Resources:  entries,
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("bundle: render manifest: %w", err)
	}
	if err := writeEntry(zw, manifestName, string(doc)+"\n"); err != nil {
		return nil, err
	}
	if err := writeEntry(zw, definitionName, b.Definition); err != nil {
		return nil, err
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

// definitionEntryName picks where the definition goes: `<slug>.yaml`, so an
// unzipped bundle is a directory whose config is recognisable by name. A resource
// may legitimately be named that too (nothing stops an author), so a taken name is
// suffixed until it is free — the definition moves rather than the resource,
// because the definition refers to resources by name and a moved resource would
// break that reference.
func definitionEntryName(b Bundle) (string, error) {
	taken := make(map[string]struct{}, len(b.Resources))
	for _, r := range b.Resources {
		taken[r.Name] = struct{}{}
	}
	base := Slug(b.Name)
	candidate := base + ".yaml"
	for i := 1; ; i++ {
		if _, clash := taken[candidate]; !clash && candidate != manifestName {
			return candidate, nil
		}
		if i > collisionLimit {
			return "", fmt.Errorf("%w: no free name for the definition entry", ErrInvalid)
		}
		candidate = fmt.Sprintf("%s-%d.yaml", base, i)
	}
}

// Slug turns a display name into a filename stem: lowercase, non-alphanumerics
// collapsed to '-', trimmed and length-bounded. Exported because the handler
// names the download from the same rule the archive's own definition entry uses.
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
		return strings.TrimSuffix(defaultDefinitionName, ".yaml")
	}
	return s
}
