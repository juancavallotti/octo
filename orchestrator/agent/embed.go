// Package agent holds Dr. Octo, the platform's own agent, as source: an Octo App
// that the orchestrator ships inside its binary and installs as an ordinary
// integration.
//
// The files beside this one are the app. They are not a template and not generated
// — `bin/octo run --config orchestrator/agent` runs exactly what a cluster runs, so
// there is one copy of the agent and no step that could let the two drift.
//
// It lives here rather than at the repo root because //go:embed cannot traverse ".."
// and the orchestrator image is built with ./orchestrator as its context.
package agent

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// bundleFS holds the app. config.yaml is named explicitly rather than matched by a
// glob, which is what keeps config_test.yaml — the dolphin suite that lives beside
// it — out of the shipped bundle and out of the digest.
//
//go:embed config.yaml skills
var bundleFS embed.FS

// definitionPath and skillsDir are where the pieces live inside the bundle.
const (
	definitionPath = "config.yaml"
	skillsDir      = "skills"
)

// Definition returns the agent's flow definition.
func Definition() (string, error) {
	raw, err := bundleFS.ReadFile(definitionPath)
	if err != nil {
		return "", fmt.Errorf("agent bundle: read %s: %w", definitionPath, err)
	}
	return string(raw), nil
}

// Skills returns the agent's skill documents, keyed by the resource name they are
// created under — "skills/blocks.md" and so on, matching the aliases the definition
// declares. Names are relative and contain a slash, which the resource layer allows.
func Skills() (map[string]string, error) {
	out := make(map[string]string)
	err := fs.WalkDir(bundleFS, skillsDir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		raw, readErr := bundleFS.ReadFile(name)
		if readErr != nil {
			return readErr
		}
		out[path.Clean(name)] = string(raw)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("agent bundle: read skills: %w", err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("agent bundle: no skills found under %s/", skillsDir)
	}
	return out, nil
}

// Digest identifies this bundle by its contents.
//
// It is what "is a newer agent available?" compares, so it has to change when any
// byte of the app does and not otherwise — including when a skill is added or
// removed, which a digest over concatenated contents alone would miss. Paths are
// sorted and each is hashed with its length, so no rearrangement of names and
// contents produces the same sum as a different bundle.
func Digest() (string, error) {
	definition, err := Definition()
	if err != nil {
		return "", err
	}
	skills, err := Skills()
	if err != nil {
		return "", err
	}

	files := map[string]string{definitionPath: definition}
	for name, content := range skills {
		files[name] = content
	}
	return DigestFiles(files), nil
}

// DigestFiles hashes a name→content map: sorted names, each with its length, so no
// rearrangement of names and contents produces another map's sum.
//
// Exported because the installer digests the *live* integration rows the same way
// and compares the two to decide whether someone has edited the agent. Two copies
// of this loop would be correct only while they stayed identical, and a change to
// one would make every installed agent report as edited.
func DigestFiles(files map[string]string) string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	sum := sha256.New()
	for _, name := range names {
		// hash.Hash never returns an error, which is why the interface's own docs say
		// so; discarding it keeps the loop readable.
		_, _ = fmt.Fprintf(sum, "%s\x00%d\x00", name, len(files[name]))
		_, _ = sum.Write([]byte(files[name]))
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// Tag is the version tag a bundle is installed under: deterministic, so
// re-installing the same bundle finds the tag it already created rather than
// making a second one. Short enough to read, long enough not to collide.
func Tag(digest string) string {
	const shortLen = 12
	if len(digest) > shortLen {
		digest = digest[:shortLen]
	}
	return "agent-" + digest
}

// Name is the integration name the agent is installed under. Integration names are
// unique case-insensitively across the install, so this is also what a collision
// with a user's own integration would report.
const Name = "Dr. Octo"

// SkillResourceKind is the resource kind a skill is stored as. They are templates
// because that is what the ai-agent block's skills slot resolves.
const SkillResourceKind = "template"

// IsSkill reports whether a resource name belongs to the bundle's skills, which is
// how the installer tells its own resources from anything a user added.
func IsSkill(name string) bool {
	return strings.HasPrefix(name, skillsDir+"/")
}
