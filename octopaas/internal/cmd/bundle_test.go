package cmd

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func zipOf(t *testing.T, files map[string]string) []byte {

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte(content))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A bundle from the server must not be able to write outside the folder you
// pulled into, however its entries are named.
func TestUnpackRefusesToEscapeItsFolder(t *testing.T) {

	for _, name := range []string{"../outside.yaml", "flows/../../outside.yaml"} {
		t.Run(name, func(t *testing.T) {

			dir := t.TempDir()
			_, err := unpack(zipOf(t, map[string]string{name: "x"}), filepath.Join(dir, "in"))
			if err == nil || !strings.Contains(err.Error(), "outside its folder") {
				t.Fatalf("an escaping entry should be refused, got %v", err)
			}
			if _, statErr := os.Stat(filepath.Join(dir, "outside.yaml")); statErr == nil {
				t.Error("the escaping entry was written anyway")
			}
		})
	}
}

// What pull unpacks, push packs back the same, minus what is hidden: a pulled
// folder can grow a .git or a .env without either going up to the platform.
func TestPackAndUnpackRoundTrip(t *testing.T) {

	dir := t.TempDir()
	n, err := unpack(zipOf(t, map[string]string{"octo.yaml": "flows: []", "resources/env/prod.env": "A=1"}), dir)
	if err != nil || n != 2 {
		t.Fatalf("unpack: %d files, %v", n, err)
	}
	_ = os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=1"), 0o644)

	data, count, err := pack(dir)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("pack should take the two integration files and nothing hidden, took %d", count)
	}
	r, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	var names []string
	for _, f := range r.File {
		names = append(names, f.Name)
	}
	if strings.Join(names, ",") != "octo.yaml,resources/env/prod.env" {
		t.Errorf("packed %v", names)
	}
}
