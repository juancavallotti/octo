// Package config reads and writes ~/.octopaas/config.yaml, and decides which
// platform and which credential a command talks to.
//
// The file holds named profiles, one per platform you work with. What a command
// actually uses is resolved from flags, then the environment, then the file, so
// a CI job can drive octopaas from OCTOPAAS_URL and OCTOPAAS_KEY alone without a
// config file ever being written by hand.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// DefaultURL is the platform a fresh config points at.
const DefaultURL = "https://api.octopaas.dev"

// DefaultProfile is the profile a fresh config creates and selects.
const DefaultProfile = "default"

// The environment variables octopaas reads. They are documented, with their
// precedence, on the CLI's guide page.
const (
	EnvHome    = "OCTOPAAS_HOME"
	EnvProfile = "OCTOPAAS_PROFILE"
	EnvURL     = "OCTOPAAS_URL"
	EnvKey     = "OCTOPAAS_KEY"
)

// Profile is one platform: where its API is, and optionally an API key to call
// it with. A profile without a key signs in with `octopaas auth login`.
type Profile struct {
	URL    string `yaml:"url"`
	APIKey string `yaml:"apiKey,omitempty"`
}

// File is config.yaml.
type File struct {
	Current  string             `yaml:"current"`
	Profiles map[string]Profile `yaml:"profiles"`
}

// header is written above the file every time it is saved, so the guidance a
// person reads on first open survives octopaas rewriting it.
const header = `# octopaas configuration.
#
# Each profile is one platform. Pick one per command with --profile, per shell
# with OCTOPAAS_PROFILE, or make it the default with ` + "`octopaas profile use`" + `.
#
# People sign in with ` + "`octopaas auth login`" + ` and need no apiKey here: the tokens
# it gets are kept in credentials.yaml beside this file. An apiKey is for
# automation, and in CI you would rather not have a file at all: set
# OCTOPAAS_URL and OCTOPAAS_KEY instead.
`

// Home is the directory octopaas keeps its files in: OCTOPAAS_HOME when set,
// ~/.octopaas otherwise.
func Home(getenv func(string) string) (string, error) {

	if dir := getenv(EnvHome); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find your home directory (set %s to choose one): %w", EnvHome, err)
	}
	return filepath.Join(home, ".octopaas"), nil
}

// Path is where config.yaml lives inside home.
func Path(home string) string {
	return filepath.Join(home, "config.yaml")
}

// Fresh is the configuration a first run writes.
func Fresh() *File {
	return &File{
		Current:  DefaultProfile,
		Profiles: map[string]Profile{DefaultProfile: {URL: DefaultURL}},
	}
}

// Load reads config.yaml from home, writing a fresh one first when there is
// none. created says whether this was that first run.
func Load(home string) (ret *File, created bool, err error) {

	data, err := os.ReadFile(Path(home))
	if errors.Is(err, os.ErrNotExist) {
		ret = Fresh()
		if err := ret.Save(home); err != nil {
			return nil, false, err
		}
		return ret, true, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", Path(home), err)
	}

	ret = &File{}
	if err := yaml.Unmarshal(data, ret); err != nil {
		return nil, false, fmt.Errorf("%s is not valid YAML: %w", Path(home), err)
	}
	if ret.Profiles == nil {
		ret.Profiles = map[string]Profile{}
	}
	return ret, false, nil
}

// Save writes f to home. The file can hold an API key, so it is only readable
// by you.
func (f *File) Save(home string) error {

	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", home, err)
	}
	body, err := yaml.Marshal(f)
	if err != nil {
		return fmt.Errorf("render the configuration: %w", err)
	}
	return writeFile(Path(home), append([]byte(header+"\n"), body...))
}

// Names lists the profiles in a stable order.
func (f *File) Names() []string {

	ret := make([]string, 0, len(f.Profiles))
	for name := range f.Profiles {
		ret = append(ret, name)
	}
	sort.Strings(ret)
	return ret
}

// writeFile replaces path atomically, so an interrupted save never leaves half a
// config behind.
func writeFile(path string, data []byte) error {

	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return fmt.Errorf("save %s: %w", path, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("save %s: %w", path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("save %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("save %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("save %s: %w", path, err)
	}
	return nil
}
