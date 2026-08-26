// Package bundle is the orchestrator feature module for whole-integration
// import and export: one zip archive carrying an integration's definition and
// every resource it refers to, so an integration can leave the platform and come
// back — or move between installs — as a single file.
//
// The archive is laid out the way the runtime already expects to find these files
// on disk: the definition YAML at the root, each resource written at its
// path-like name relative to it (`.env.dev`, `templates/welcome.tmpl`). Unzipping
// a bundle therefore produces a directory a local `octo` can run. What that
// layout cannot express — the integration's display name, and each resource's
// kind, which is a stored property rather than something a filename guarantees —
// lives in a small manifest beside them.
package bundle

import "strings"

const (
	// manifestName is the manifest's entry name in the archive. Dotted so it sorts
	// and reads as metadata rather than as one of the integration's own files, and
	// harmless to a runtime that unzips the bundle and ignores it.
	manifestName = "octo-bundle.json"
	// manifestVersion is the format version written into every bundle. A reader
	// rejects a version it does not know rather than guessing at the layout.
	manifestVersion = 1
	// defaultDefinitionName is the definition's entry name when the integration's
	// name slugifies to nothing, or when the slug collides with a resource.
	defaultDefinitionName = "integration.yaml"
	// envPrefix is the filename convention that marks an env resource, used only
	// when reading a manifest-less archive. It mirrors the editor's `guessKind`
	// and the standalone loader's convention.
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

// Bundle is an integration as a portable unit: its display name, its definition
// YAML, and its resources. Tag is set only for a bundle exported from a version
// tag, where it records which tag the contents were frozen at; it is descriptive
// metadata, and importing a tagged bundle produces an ordinary working copy.
type Bundle struct {
	Name       string
	Tag        string
	Definition string
	Resources  []File
}

// manifest is the archive's metadata entry. It carries what the file layout
// cannot: the integration's display name, the tag an export was taken from, which
// entry is the definition, and each resource's kind.
type manifest struct {
	Version    int             `json:"version"`
	Name       string          `json:"name"`
	Tag        string          `json:"tag,omitempty"`
	Definition string          `json:"definition"`
	Resources  []manifestEntry `json:"resources"`
}

// manifestEntry describes one resource. The name is both the id the definition
// refers to it by and its entry name in the archive, so no second path is stored:
// one name that can disagree with another is a bug waiting to happen.
type manifestEntry struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// guessKind infers a resource's kind from its name the way the editor and the
// standalone loader do: a `.env`-convention file is env, everything else is a
// template. Used only for archives without a manifest — a bundle this
// orchestrator wrote always states the kind outright.
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
