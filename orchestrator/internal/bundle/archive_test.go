package bundle

import (
	"archive/zip"
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/projectfile"
)

// zipOf builds an archive from name/content pairs, for the reader's tests.
func zipOf(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create %q: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write %q: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return buf.Bytes()
}

// entryNames lists an archive's entries, so a test can assert the layout a
// developer sees after unzipping rather than only what Read gives back.
func entryNames(t *testing.T, data []byte) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	return names
}

// configs is a shorthand for a bundle's config files.
func configs(pathContent ...string) []projectfile.File {
	ret := make([]projectfile.File, 0, len(pathContent)/2)
	for i := 0; i+1 < len(pathContent); i += 2 {
		ret = append(ret, projectfile.File{Path: pathContent[i], Content: pathContent[i+1]})
	}
	return ret
}

func TestWriteReadRoundTrip(t *testing.T) {
	in := Bundle{
		Name:    "Order Sync",
		Configs: configs("octo.yaml", "service:\n  name: order-sync\n", "orders.yaml", "flows: []\n"),
		Resources: []File{
			{Kind: kindEnv, Name: ".env.dev", Content: "TOKEN=abc\n"},
			{Kind: kindTemplate, Name: "templates/welcome.tmpl", Content: "hi {{ msg }}\n"},
		},
	}
	data, err := Write(in)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := Read(data)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Name != in.Name || !slices.Equal(got.Configs, in.Configs) {
		t.Fatalf("Read = %+v, want name/configs from %+v", got, in)
	}
	if !slices.Equal(got.Resources, in.Resources) {
		t.Errorf("resources = %+v, want %+v", got.Resources, in.Resources)
	}
}

// Each config file lands at its own path, beside the resources at theirs:
// unzipping a bundle has to produce a directory a local runtime can load.
func TestWriteLaysOutRunnableDirectory(t *testing.T) {
	data, err := Write(Bundle{
		Name:      "Order Sync",
		Configs:   configs("octo.yaml", "service:\n  name: order-sync\n", "orders.yaml", "flows: []\n"),
		Resources: []File{{Kind: kindTemplate, Name: "templates/welcome.tmpl", Content: "x"}},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	want := map[string]bool{manifestName: true, "octo.yaml": true, "orders.yaml": true, "templates/welcome.tmpl": true}
	for _, name := range entryNames(t, data) {
		if !want[name] {
			t.Errorf("unexpected entry %q", name)
		}
		delete(want, name)
	}
	for name := range want {
		t.Errorf("missing entry %q", name)
	}
}

// A path that is not config would come back as something else on import, so
// writing it under the config list is refused.
func TestWriteRejectsAConfigPathThatIsNotConfig(t *testing.T) {
	for _, path := range []string{"flows/orders.yaml", "orders_test.yaml", "../escape.yaml"} {
		if _, err := Write(Bundle{Configs: configs(path, "a: b\n")}); !errors.Is(err, ErrInvalid) {
			t.Errorf("Write(%q) err = %v, want ErrInvalid", path, err)
		}
	}
}

// An archive written before bundles carried several config files names its one
// definition entry instead of listing configs, and still imports.
func TestReadSingleDefinitionManifest(t *testing.T) {
	data := zipOf(t, map[string]string{
		manifestName:      `{"version":1,"name":"Old","definition":"old.yaml","resources":[{"name":".env","kind":"env"}]}`,
		"old.yaml":        "flows: []\n",
		".env":            "A=1\n",
		"unlisted.yaml":   "ignored: true\n",
		"templates/x.txt": "ignored\n",
	})
	got, err := Read(data)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Name != "Old" || !slices.Equal(got.Configs, configs("old.yaml", "flows: []\n")) {
		t.Errorf("Read = %+v, want old.yaml as the one config", got)
	}
	if len(got.Resources) != 1 || got.Resources[0].Name != ".env" {
		t.Errorf("resources = %+v, want only the listed .env", got.Resources)
	}
}

// Without a manifest the archive is read the way the runtime reads a directory:
// every root-level, non-test YAML is config.
func TestReadManifestlessArchiveInfersRoles(t *testing.T) {
	data := zipOf(t, map[string]string{
		"octo.yaml":             "service:\n  name: mine\n",
		"orders.yml":            "flows: []\n",
		".env.prod":             "A=1\n",
		"templates/hello.tmpl":  "hello\n",
		"templates/nested.yaml": "not config\n",
	})
	got, err := Read(data)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !slices.Equal(got.Configs, configs("octo.yaml", "service:\n  name: mine\n", "orders.yml", "flows: []\n")) {
		t.Fatalf("configs = %+v", got.Configs)
	}
	if got.Name != "" {
		t.Errorf("Name = %q, want empty so the caller names the import", got.Name)
	}
	want := map[string]string{
		".env.prod":             kindEnv,
		"templates/hello.tmpl":  kindTemplate,
		"templates/nested.yaml": kindTemplate,
	}
	if len(got.Resources) != len(want) {
		t.Fatalf("resources = %+v, want %d", got.Resources, len(want))
	}
	for _, r := range got.Resources {
		if kind, ok := want[r.Name]; !ok || kind != r.Kind {
			t.Errorf("resource %q kind = %q, want %q", r.Name, r.Kind, kind)
		}
	}
}

func TestReadRejects(t *testing.T) {
	manifestFor := func(body string) map[string]string {
		return map[string]string{manifestName: body, "def.yaml": "a: b\n", "templates/x.tmpl": "x"}
	}
	cases := []struct {
		name    string
		entries map[string]string
		want    error
	}{
		{
			name:    "no root yaml and no manifest",
			entries: map[string]string{"templates/a.tmpl": "x"},
			want:    ErrInvalid,
		},
		{
			name:    "escaping entry",
			entries: map[string]string{"../escape.yaml": "a: b\n"},
			want:    ErrInvalid,
		},
		{
			name:    "manifest does not parse",
			entries: manifestFor("{"),
			want:    ErrInvalid,
		},
		{
			name:    "unknown manifest version",
			entries: manifestFor(`{"version":99,"configs":["def.yaml"]}`),
			want:    ErrInvalid,
		},
		{
			name:    "manifest lists no config file",
			entries: manifestFor(`{"version":2,"configs":[]}`),
			want:    ErrInvalid,
		},
		{
			name:    "manifest lists a missing config file",
			entries: manifestFor(`{"version":2,"configs":["def.yaml","gone.yaml"]}`),
			want:    ErrInvalid,
		},
		{
			name:    "manifest lists a config file that is not config",
			entries: manifestFor(`{"version":2,"configs":["templates/x.tmpl"]}`),
			want:    ErrInvalid,
		},
		{
			name:    "single-definition manifest names a missing definition",
			entries: manifestFor(`{"version":1,"definition":"gone.yaml"}`),
			want:    ErrInvalid,
		},
		{
			name:    "manifest lists a missing resource",
			entries: manifestFor(`{"version":2,"configs":["def.yaml"],"resources":[{"name":"gone","kind":"env"}]}`),
			want:    ErrInvalid,
		},
		{
			name:    "manifest states an unknown kind",
			entries: manifestFor(`{"version":2,"configs":["def.yaml"],"resources":[{"name":"templates/x.tmpl","kind":"secret"}]}`),
			want:    ErrInvalid,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Read(zipOf(t, tc.entries)); !errors.Is(err, tc.want) {
				t.Fatalf("Read err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestReadRejectsSomethingThatIsNotAZip(t *testing.T) {
	if _, err := Read([]byte("name: not-a-zip\n")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Read err = %v, want ErrInvalid", err)
	}
}

// A small archive that expands past the limit is refused rather than read into
// memory: the upload cap alone cannot bound what a zip decompresses to.
func TestReadRejectsAnArchiveThatExpandsPastTheLimit(t *testing.T) {
	data := zipOf(t, map[string]string{
		"big.yaml": strings.Repeat("a", maxTotalBytes+1),
	})
	if _, err := Read(data); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Read err = %v, want ErrTooLarge", err)
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Order Sync":      "order-sync",
		"  Spaced  Out  ": "spaced-out",
		"a/b\\c":          "a-b-c",
		"":                "integration",
		"!!!":             "integration",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

// A resource may legally be named like the manifest. Writing it would produce an
// archive that is a valid zip and an unreadable bundle, so the export fails
// outright instead.
func TestWriteRejectsAResourceUsingTheManifestName(t *testing.T) {
	_, err := Write(Bundle{
		Name:      "Order Sync",
		Configs:   configs("a.yaml", "a: b\n"),
		Resources: []File{{Kind: kindTemplate, Name: manifestName, Content: "not a manifest"}},
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Write err = %v, want ErrInvalid", err)
	}
}

// A zip can carry two entries under one name. Keeping the last silently would let
// a hand-made archive shadow the manifest with a resource.
func TestReadRejectsDuplicateEntryNames(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, content := range []string{"a: 1\n", "a: 2\n"} {
		w, err := zw.Create("def.yaml")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := Read(buf.Bytes()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Read err = %v, want ErrInvalid", err)
	}
}
