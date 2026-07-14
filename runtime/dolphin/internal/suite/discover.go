package suite

// This file pairs a suite with the flows it exercises. The pairing is the whole of
// what `dolphin test <path>` has to figure out, and it follows one rule:
//
//	the config is exactly what you point at.
//
// Point at a directory and the directory is the config, as `octo --config <dir>`
// means it; point at a flow file and that file is the config, as `octo --config
// <file>` means it. Nothing is inferred beyond the companion naming, so what dolphin
// hands octo is always something the user could have typed themselves.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/juancavallotti/octo/runtime/core/runtime"
)

// Target is one suite and the config it runs against — everything `octo invoke` needs
// but the case itself.
type Target struct {
	// Config is what to hand `octo --config`: a flow file, or a directory of them.
	Config string
	File   File
}

// Discover resolves the paths a user named into the suites to run.
//
// A path is taken as it is meant:
//
//	./flows            a directory  → the config; every *_test.yaml in it is a suite
//	flows/orders.yaml  a flow file  → the config; its companion orders_test.yaml is the suite
//	flows/orders_test.yaml  a suite → run it, against the flow file beside it
//
// configOverride, when set, replaces the config for every target — which is how a
// suite that lives apart from its flows names them, and how you say "this flow file,
// that test file" explicitly:
//
//	dolphin test --config flows/orders.yaml tests/smoke_test.yaml
//
// Naming a flow file and its own test file together resolves to the same target twice
// and is deduped, so `dolphin test orders.yaml orders_test.yaml` does what it looks
// like rather than running the suite twice.
func Discover(paths []string, configOverride string) ([]Target, error) {
	if len(paths) == 0 {
		paths = []string{"."}
	}

	var targets []Target
	for _, path := range paths {
		found, err := discoverOne(path)
		if err != nil {
			return nil, err
		}
		targets = append(targets, found...)
	}

	if configOverride != "" {
		if _, err := os.Stat(configOverride); err != nil {
			return nil, fmt.Errorf("read config %q: %w", configOverride, err)
		}
		for i := range targets {
			targets[i].Config = configOverride
		}
	}
	return loadAll(dedupe(targets))
}

// discoverOne resolves a single path into the suites it names.
func discoverOne(path string) ([]Target, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	if info.IsDir() {
		return suitesIn(path)
	}

	// A test file names itself. Its config is the flow file beside it, when there is
	// one, and otherwise the directory it lives in — which is what a suite covering
	// flows split across several files wants.
	if runtime.IsTestFile(path) {
		return []Target{{Config: configForSuite(path), File: File{Path: path}}}, nil
	}

	// A flow file names its companion suite. Not finding one is an error, not an empty
	// run: `dolphin test orders.yaml` asked for orders to be tested, and reporting
	// "0 cases, ok" would be a green run that tested nothing.
	companion := companionSuite(path)
	if _, err := os.Stat(companion); err != nil {
		return nil, fmt.Errorf("no test file for %s (expected %s)", path, companion)
	}
	return []Target{{Config: path, File: File{Path: companion}}}, nil
}

// suitesIn returns every suite directly in dir, sorted, with dir as their config.
// Subdirectories are skipped, matching how `octo --config <dir>` reads a directory.
func suitesIn(dir string) ([]Target, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read directory %q: %w", dir, err)
	}

	var targets []Target
	for _, entry := range entries {
		if entry.IsDir() || !isYAML(entry.Name()) || !runtime.IsTestFile(entry.Name()) {
			continue
		}
		targets = append(targets, Target{
			Config: dir,
			File:   File{Path: filepath.Join(dir, entry.Name())},
		})
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no *_test.yaml files in %q", dir)
	}
	return targets, nil
}

// companionSuite is the suite that tests the flow file at path: orders.yaml is tested
// by orders_test.yaml, keeping the extension it was written with.
func companionSuite(flowPath string) string {
	ext := filepath.Ext(flowPath)
	return strings.TrimSuffix(flowPath, ext) + runtime.TestFileSuffix + ext
}

// configForSuite is the config a suite runs against when it was named directly: the
// flow file it is the companion of, else the directory it lives in.
func configForSuite(suitePath string) string {
	ext := filepath.Ext(suitePath)
	flow := strings.TrimSuffix(strings.TrimSuffix(suitePath, ext), runtime.TestFileSuffix) + ext
	if _, err := os.Stat(flow); err == nil {
		return flow
	}
	return filepath.Dir(suitePath)
}

// pairing identifies a target by what it actually runs, which is the two paths. Target
// itself cannot be a map key: File carries maps, so it is not comparable.
type pairing struct{ config, suite string }

// dedupe drops targets naming the same suite against the same config, so that pointing
// at a flow file AND its own test file — which resolve to the same pair — runs the
// suite once.
func dedupe(targets []Target) []Target {
	seen := make(map[pairing]bool, len(targets))
	unique := make([]Target, 0, len(targets))
	for _, target := range targets {
		key := pairing{config: target.Config, suite: target.File.Path}
		if seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, target)
	}
	// Sorted by suite, so a run reports its files in the same order every time.
	sort.Slice(unique, func(i, j int) bool { return unique[i].File.Path < unique[j].File.Path })
	return unique
}

// loadAll reads and validates every discovered suite. It happens here, before the run
// starts, so a malformed suite stops the whole run rather than failing in the middle of
// it with half the report already printed.
func loadAll(targets []Target) ([]Target, error) {
	loaded := make([]Target, 0, len(targets))
	for _, target := range targets {
		file, err := Load(target.File.Path)
		if err != nil {
			return nil, err
		}
		target.File = file
		loaded = append(loaded, target)
	}
	return loaded, nil
}

// isYAML reports whether name has a YAML extension.
func isYAML(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".yaml" || ext == ".yml"
}
