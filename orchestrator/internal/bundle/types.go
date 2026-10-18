// Package bundle is the orchestrator feature module for whole-integration
// import and export: one zip archive carrying an integration's config files and
// every resource they refer to, so an integration can leave the platform and come
// back — or move between installs — as a single file.
//
// The archive is laid out the way these files are expected on disk: each config
// file at the root under its own name, each resource at its path-like name
// relative to it (`.env.dev`, `templates/welcome.tmpl`), so unzipping one produces
// a runnable directory. What that layout cannot express — the display name, and
// each resource's kind — lives in a small manifest beside them.
package bundle

import (
	"strings"

	"github.com/juancavallotti/octo/orchestrator/internal/projectfile"
)

const (
	// manifestName is the manifest's entry name in the archive. Dotted so it sorts
	// and reads as metadata rather than as one of the integration's own files, and
	// harmless to a runtime that unzips the bundle and ignores it.
	manifestName = "octo-bundle.json"
	// manifestVersion is the format version written into every bundle. A reader
	// rejects a version it does not know rather than guessing at the layout.
	manifestVersion = 2
	// singleDefinitionVersion is the earlier format, whose manifest names one
	// definition entry rather than listing config files. It is still read, since
	// archives written in it are sitting on people's disks.
	singleDefinitionVersion = 1
	// fallbackSlug is the slug of a name that slugifies to nothing.
	fallbackSlug = "integration"
	// envPrefix is the filename convention that marks an env resource, used only
	// when reading a manifest-less archive.
	envPrefix = ".env"
)

// Kind values a resource may carry. They mirror resource.KindEnv/KindTemplate,
// duplicated as plain strings so the archive format — which is written to disk
// and read back by other tools — does not depend on the storage module.
const (
	kindEnv      = "env"
	kindTemplate = "template"
)

// File is one resource inside a bundle: the path-like name the definition refers
// to it by, its kind, and its bytes.
type File struct {
	Kind    string
	Name    string
	Content string
}

// Bundle is an integration as a portable unit: its display name, its config
// files, and its resources. Tag is set only for a bundle exported from a version
// tag, where it records which tag the contents were frozen at; it is descriptive
// metadata, and importing a tagged bundle produces an ordinary working copy.
type Bundle struct {
	Name      string
	Tag       string
	Configs   []projectfile.File
	Resources []File
}

// manifest is the archive's metadata entry. It carries what the file layout
// cannot: the integration's display name, the tag an export was taken from, which
// entries are config files, and each resource's kind. Definition is read only
// from a singleDefinitionVersion manifest, where it names the one config file.
type manifest struct {
	Version    int             `json:"version"`
	Name       string          `json:"name"`
	Tag        string          `json:"tag,omitempty"`
	Configs    []string        `json:"configs"`
	Definition string          `json:"definition,omitempty"`
	Resources  []manifestEntry `json:"resources"`
}

// manifestEntry describes one resource. The name is both the id the definition
// refers to it by and its entry name in the archive, so no second path is stored:
// one name that can disagree with another is a bug waiting to happen.
type manifestEntry struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// guessKind infers a resource's kind from its name: a `.env`-convention file is env,
// everything else is a template. Used only for archives without a manifest — a bundle
// this orchestrator wrote always states the kind outright.
//
// The name may be a path, so only the last segment decides.
func guessKind(name string) string {
	base := name
	if i := strings.LastIndex(name, "/"); i >= 0 {
		base = name[i+1:]
	}
	if strings.HasPrefix(base, envPrefix) {
		return kindEnv
	}
	return kindTemplate
}
