package bundle

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"
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

func TestWriteReadRoundTrip(t *testing.T) {
	in := Bundle{
		Name:       "Order Sync",
		Definition: "name: order-sync\n",
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
	if got.Name != in.Name || got.Definition != in.Definition {
		t.Fatalf("Read = %+v, want name/definition from %+v", got, in)
	}
	if len(got.Resources) != len(in.Resources) {
		t.Fatalf("got %d resources, want %d", len(got.Resources), len(in.Resources))
	}
	for i, want := range in.Resources {
		if got.Resources[i] != want {
			t.Errorf("resource %d = %+v, want %+v", i, got.Resources[i], want)
		}
	}
}

// The definition lands at a name derived from the integration, beside the
// resources at their own path-like names: unzipping a bundle has to produce a
// directory a local runtime can load.
func TestWriteLaysOutRunnableDirectory(t *testing.T) {
	data, err := Write(Bundle{
		Name:       "Order Sync",
		Definition: "name: order-sync\n",
		Resources:  []File{{Kind: kindTemplate, Name: "templates/welcome.tmpl", Content: "x"}},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	want := map[string]bool{manifestName: true, "order-sync.yaml": true, "templates/welcome.tmpl": true}
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

// A resource may be named like the definition would be. The definition moves,
// because the definition refers to resources by name and moving one would break
// that reference.
func TestWriteMovesDefinitionOffAClashingResourceName(t *testing.T) {
	data, err := Write(Bundle{
		Name:       "Order Sync",
		Definition: "name: order-sync\n",
		Resources:  []File{{Kind: kindTemplate, Name: "order-sync.yaml", Content: "a template"}},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Read(data)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Definition != "name: order-sync\n" {
		t.Errorf("definition = %q, want the definition, not the resource", got.Definition)
	}
	if len(got.Resources) != 1 || got.Resources[0].Content != "a template" {
		t.Errorf("resources = %+v, want the clashing template preserved", got.Resources)
	}
}

// A name that slugifies to nothing still has to produce a usable entry name.
func TestWriteNamesDefinitionWhenTheNameSlugifiesToNothing(t *testing.T) {
	data, err := Write(Bundle{Name: "///", Definition: "a: b\n"})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	found := false
	for _, name := range entryNames(t, data) {
		if name == defaultDefinitionName {
			found = true
		}
	}
	if !found {
		t.Errorf("entries = %v, want one named %q", entryNames(t, data), defaultDefinitionName)
	}
}

func TestReadManifestlessArchiveInfersRoles(t *testing.T) {
	data := zipOf(t, map[string]string{
		"my-integration.yaml":   "name: mine\n",
		".env.prod":             "A=1\n",
		"templates/hello.tmpl":  "hello\n",
		"templates/nested.yaml": "not the definition\n",
	})
	got, err := Read(data)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Definition != "name: mine\n" {
		t.Fatalf("definition = %q", got.Definition)
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
		return map[string]string{manifestName: body, "def.yaml": "a: b\n"}
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
			name:    "two root yamls and no manifest",
			entries: map[string]string{"a.yaml": "a: 1\n", "b.yml": "b: 2\n"},
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
			entries: manifestFor(`{"version":99,"definition":"def.yaml"}`),
			want:    ErrInvalid,
		},
		{
			name:    "manifest names a missing definition",
			entries: manifestFor(`{"version":1,"definition":"gone.yaml"}`),
			want:    ErrInvalid,
		},
		{
			name:    "manifest names a missing resource",
			entries: manifestFor(`{"version":1,"definition":"def.yaml","resources":[{"name":"gone","kind":"env"}]}`),
			want:    ErrInvalid,
		},
		{
			name:    "manifest states an unknown kind",
			entries: manifestFor(`{"version":1,"definition":"def.yaml","resources":[{"name":"def.yaml","kind":"secret"}]}`),
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
		Name:       "Order Sync",
		Definition: "a: b\n",
		Resources:  []File{{Kind: kindTemplate, Name: manifestName, Content: "not a manifest"}},
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
