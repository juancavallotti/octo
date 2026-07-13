package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeOcto writes an executable stub named octo into dir and returns its path. A
// shell script is enough: every test here cares about how a binary is *found*, and
// the one that cares about running it only reads what it printed.
func fakeOcto(t *testing.T, dir, output string, exitCode int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake octo is a shell script; resolution on Windows is covered by exec.LookPath")
	}
	path := filepath.Join(dir, "octo")
	script := fmt.Sprintf("#!/bin/sh\necho '%s'\nexit %d\n", output, exitCode)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil { //nolint:gosec // a test fixture must be executable
		t.Fatalf("write fake octo: %v", err)
	}
	return path
}

// samePath compares two paths after resolving symlinks. On macOS t.TempDir() hands
// back a /var/... path while os.Getwd reports the /private/var/... it resolves to,
// so comparing the strings as they come would fail for reasons that have nothing to
// do with resolution.
func samePath(t *testing.T, got, want string) bool {
	t.Helper()
	gotReal, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("resolve %q: %v", got, err)
	}
	wantReal, err := filepath.EvalSymlinks(want)
	if err != nil {
		t.Fatalf("resolve %q: %v", want, err)
	}
	return gotReal == wantReal
}

// isolate points the resolver at nothing: no override, an empty working directory,
// and a PATH with no octo on it. Each test then adds back exactly the one source it
// is about, so a pass can only come from that source.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv(octoPathEnv, "")
	t.Setenv("PATH", t.TempDir())
	t.Chdir(t.TempDir())
}

func TestResolveOctoFindsTheBinary(t *testing.T) {
	t.Run("$OCTO_PATH naming the binary", func(t *testing.T) {
		isolate(t)
		want := fakeOcto(t, t.TempDir(), "octo 9.9.9", 0)
		t.Setenv(octoPathEnv, want)

		bin, err := resolveOcto()
		if err != nil {
			t.Fatalf("resolveOcto: %v", err)
		}
		if !samePath(t, bin.Path, want) {
			t.Errorf("path = %q, want %q", bin.Path, want)
		}
		if bin.Source != sourceEnv {
			t.Errorf("source = %q, want %q", bin.Source, sourceEnv)
		}
	})

	t.Run("$OCTO_PATH naming the directory holding it", func(t *testing.T) {
		isolate(t)
		dir := t.TempDir()
		want := fakeOcto(t, dir, "octo 9.9.9", 0)
		t.Setenv(octoPathEnv, dir)

		bin, err := resolveOcto()
		if err != nil {
			t.Fatalf("resolveOcto: %v", err)
		}
		if !samePath(t, bin.Path, want) {
			t.Errorf("path = %q, want %q", bin.Path, want)
		}
		if bin.Source != sourceEnv {
			t.Errorf("source = %q, want %q", bin.Source, sourceEnv)
		}
	})

	t.Run("the current directory", func(t *testing.T) {
		isolate(t)
		dir := t.TempDir()
		want := fakeOcto(t, dir, "octo 9.9.9", 0)
		t.Chdir(dir)

		bin, err := resolveOcto()
		if err != nil {
			t.Fatalf("resolveOcto: %v", err)
		}
		if !samePath(t, bin.Path, want) {
			t.Errorf("path = %q, want %q", bin.Path, want)
		}
		if bin.Source != sourceWorkdir {
			t.Errorf("source = %q, want %q", bin.Source, sourceWorkdir)
		}
	})

	t.Run("PATH", func(t *testing.T) {
		isolate(t)
		dir := t.TempDir()
		want := fakeOcto(t, dir, "octo 9.9.9", 0)
		t.Setenv("PATH", dir)

		bin, err := resolveOcto()
		if err != nil {
			t.Fatalf("resolveOcto: %v", err)
		}
		if !samePath(t, bin.Path, want) {
			t.Errorf("path = %q, want %q", bin.Path, want)
		}
		if bin.Source != sourcePath {
			t.Errorf("source = %q, want %q", bin.Source, sourcePath)
		}
	})
}

func TestResolveOctoPrefersTheEarlierSource(t *testing.T) {
	t.Run("$OCTO_PATH over the current directory and PATH", func(t *testing.T) {
		isolate(t)
		override := fakeOcto(t, t.TempDir(), "octo 9.9.9", 0)
		pathDir := t.TempDir()
		fakeOcto(t, pathDir, "octo 0.0.0", 0)
		workDir := t.TempDir()
		fakeOcto(t, workDir, "octo 0.0.0", 0)

		t.Setenv(octoPathEnv, override)
		t.Setenv("PATH", pathDir)
		t.Chdir(workDir)

		bin, err := resolveOcto()
		if err != nil {
			t.Fatalf("resolveOcto: %v", err)
		}
		if !samePath(t, bin.Path, override) {
			t.Errorf("path = %q, want the $OCTO_PATH binary %q", bin.Path, override)
		}
	})

	t.Run("the current directory over PATH", func(t *testing.T) {
		isolate(t)
		pathDir := t.TempDir()
		fakeOcto(t, pathDir, "octo 0.0.0", 0)
		workDir := t.TempDir()
		want := fakeOcto(t, workDir, "octo 9.9.9", 0)

		t.Setenv("PATH", pathDir)
		t.Chdir(workDir)

		bin, err := resolveOcto()
		if err != nil {
			t.Fatalf("resolveOcto: %v", err)
		}
		if !samePath(t, bin.Path, want) {
			t.Errorf("path = %q, want the working-directory binary %q", bin.Path, want)
		}
	})
}

// A broken $OCTO_PATH must fail the run, not silently hand back some other octo:
// the user named a build, and testing against a different one behind their back is
// the worst outcome available. This is the test that keeps that promise.
func TestResolveOctoDoesNotFallBackFromABrokenOverride(t *testing.T) {
	cases := []struct {
		name     string
		override func(t *testing.T) string
	}{
		{
			name:     "the path does not exist",
			override: func(t *testing.T) string { t.Helper(); return filepath.Join(t.TempDir(), "nope", "octo") },
		},
		{
			name: "the file is not executable",
			override: func(t *testing.T) string {
				t.Helper()
				path := filepath.Join(t.TempDir(), "octo")
				if err := os.WriteFile(path, []byte("not a binary"), 0o600); err != nil {
					t.Fatalf("write file: %v", err)
				}
				return path
			},
		},
		{
			name:     "the directory holds no octo",
			override: func(t *testing.T) string { t.Helper(); return t.TempDir() },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			// A perfectly good octo on PATH, which must NOT be picked up.
			pathDir := t.TempDir()
			fakeOcto(t, pathDir, "octo 9.9.9", 0)
			t.Setenv("PATH", pathDir)
			t.Setenv(octoPathEnv, tc.override(t))

			bin, err := resolveOcto()
			if err == nil {
				t.Fatalf("resolveOcto = %+v, want an error", bin)
			}
			if !strings.Contains(err.Error(), octoPathEnv) {
				t.Errorf("error %q does not name %s, so the user cannot tell what to fix", err, octoPathEnv)
			}
		})
	}
}

func TestResolveOctoReportsWhereItLooked(t *testing.T) {
	isolate(t)

	_, err := resolveOcto()
	if err == nil {
		t.Fatal("resolveOcto = nil error, want octo not found")
	}
	for _, want := range []string{octoPathEnv, "PATH", "not found"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestOctoBinVersion(t *testing.T) {
	t.Run("returns what octo printed", func(t *testing.T) {
		bin := octoBin{Path: fakeOcto(t, t.TempDir(), "octo 9.9.9 (built today)", 0), Source: sourceEnv}

		got, err := bin.version(context.Background())
		if err != nil {
			t.Fatalf("version: %v", err)
		}
		if got != "octo 9.9.9 (built today)" {
			t.Errorf("version = %q", got)
		}
	})

	t.Run("fails when octo does not run", func(t *testing.T) {
		bin := octoBin{Path: fakeOcto(t, t.TempDir(), "boom", 1), Source: sourceEnv}

		if _, err := bin.version(context.Background()); err == nil {
			t.Fatal("version = nil error, want the failing binary to be reported")
		}
	})
}

func TestExitCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{name: "no error", err: nil, want: exitOK},
		{name: "a plain error is a usage error", err: errors.New("boom"), want: exitUsage},
		{name: "a usage error", err: usageErr("boom"), want: exitUsage},
		{name: "not implemented", err: &exitError{code: exitNotImplemented, err: errNotImplemented}, want: exitNotImplemented},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCode(tc.err); got != tc.want {
				t.Errorf("exitCode = %d, want %d", got, tc.want)
			}
		})
	}
}
