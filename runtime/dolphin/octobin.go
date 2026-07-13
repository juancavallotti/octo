package main

// This file holds the one thing dolphin cannot work without: finding the octo
// binary it is going to drive. Everything dolphin will eventually do — start a
// runtime, invoke a flow, assert on the result — is octo doing it, so which octo
// gets picked is the single most consequential decision of a run, and it is the
// one users hit first.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// octoPathEnv names a specific octo binary, or the directory holding one.
	octoPathEnv = "OCTO_PATH"
	// octoProbeTimeout bounds `octo version`: a probe that hangs is a broken binary
	// too, and a run that waits forever on it tells the user nothing.
	octoProbeTimeout = 10 * time.Second
	// installDocs is where someone who has no octo at all should go.
	installDocs = "https://juancavallotti.github.io/octo/getting-started/installation"
)

// octoSource is where a binary was found. It is printed, so a run says out loud
// which octo it is about to trust rather than leaving the user to guess.
type octoSource string

const (
	sourceEnv     octoSource = "$" + octoPathEnv
	sourceWorkdir octoSource = "current directory"
	sourcePath    octoSource = "PATH"
)

// octoBin is a resolved octo binary: an absolute path, and how it was found.
type octoBin struct {
	Path   string
	Source octoSource
}

// resolveOcto finds the octo binary dolphin will drive, looking in $OCTO_PATH, the
// current directory, and PATH, in that order, and stopping at the first hit.
//
// $OCTO_PATH is an override, not a hint: when it is set but does not name an
// executable, this fails instead of falling through to the other two. Running a
// test suite against a binary the user did not choose — while they believe they
// pointed us at a specific build — is a worse outcome than not running it at all.
func resolveOcto() (octoBin, error) {
	if raw := os.Getenv(octoPathEnv); raw != "" {
		return fromOverride(raw)
	}
	if bin, ok := fromWorkingDir(); ok {
		return bin, nil
	}
	return fromPath()
}

// fromOverride resolves $OCTO_PATH, which may name the binary itself or the
// directory holding it — pointing at a build directory is the more natural thing
// to type, and there is no ambiguity in accepting both.
func fromOverride(raw string) (octoBin, error) {
	path, err := filepath.Abs(raw)
	if err != nil {
		return octoBin{}, fmt.Errorf("%s=%q: %w", octoPathEnv, raw, err)
	}
	if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
		path = filepath.Join(path, octoBinaryName())
	}
	path, err = executable(path)
	if err != nil {
		return octoBin{}, fmt.Errorf("%s=%q does not point at a runnable octo binary: %w", octoPathEnv, raw, err)
	}
	return octoBin{Path: path, Source: sourceEnv}, nil
}

// fromWorkingDir looks for octo next to whatever the user is working on. Not
// finding one is not an error — it is the common case — so it reports a miss
// rather than failing the whole resolution.
func fromWorkingDir() (octoBin, bool) {
	wd, err := os.Getwd()
	if err != nil {
		return octoBin{}, false
	}
	path, err := executable(filepath.Join(wd, octoBinaryName()))
	if err != nil {
		return octoBin{}, false
	}
	return octoBin{Path: path, Source: sourceWorkdir}, true
}

// fromPath looks octo up on PATH — the installed-it-properly case.
func fromPath() (octoBin, error) {
	found, err := exec.LookPath("octo")
	if err != nil {
		return octoBin{}, notFoundError()
	}
	path, err := filepath.Abs(found)
	if err != nil {
		return octoBin{}, fmt.Errorf("resolve octo at %q: %w", found, err)
	}
	return octoBin{Path: path, Source: sourcePath}, nil
}

// notFoundError names every place we looked, because "octo not found" without that
// list leaves the user to guess which of the three knobs to turn. One line, no
// wrapping: this is logged, and a multi-line error comes back through slog with its
// newlines escaped.
func notFoundError() error {
	wd, err := os.Getwd()
	if err != nil {
		wd = "the current directory"
	}
	return fmt.Errorf(
		"octo not found: $%s is unset, there is no octo in %s, and octo is not on your PATH — "+
			"install it (%s), or point dolphin at a build with %s=/path/to/octo",
		octoPathEnv, wd, installDocs, octoPathEnv)
}

// executable returns the absolute path of the executable file at path.
//
// The check is delegated to exec.LookPath, which — given a name containing a
// separator — tests that exact file with the platform's own definition of
// "executable": the exec bit on Unix, a PATHEXT extension on Windows. A hand-rolled
// mode&0111 would be plain wrong on Windows, where the mode bits mean nothing.
func executable(path string) (string, error) {
	found, err := exec.LookPath(path)
	if err != nil {
		return "", fmt.Errorf("%w", err)
	}
	return filepath.Abs(found) //nolint:wrapcheck // filepath.Abs on a path LookPath already accepted
}

// octoBinaryName is the file name an octo binary has on this platform.
func octoBinaryName() string {
	if runtime.GOOS == "windows" {
		return "octo.exe"
	}
	return "octo"
}

// version runs `octo version` and returns what it printed. Resolving a path only
// proves a file is there: a binary for the wrong architecture, a half-finished
// download, or a directory that happens to be named octo all survive a stat and
// fail here — which is where we want them to fail, before a suite starts.
func (b octoBin) version(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, octoProbeTimeout)
	defer cancel()

	//nolint:gosec // G204: b.Path is the binary we deliberately resolved, and running it is the point.
	out, err := exec.CommandContext(ctx, b.Path, "version").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("run %q version: %w: %s", b.Path, err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("run %q version: %w", b.Path, err)
	}
	return strings.TrimSpace(string(out)), nil
}
