package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/json"
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

func packed(t *testing.T, dir string) map[string]string {

	data, _, err := pack(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range r.File {
		rc, _ := f.Open()
		var b bytes.Buffer
		_, _ = b.ReadFrom(rc)
		_ = rc.Close()
		out[f.Name] = b.String()
	}
	return out
}

// A pulled folder edited since the pull pushes as it is now: the manifest pull left
// behind lists what is on disk, keeps the kinds the platform gave its resources,
// and still carries the hidden env resources the platform sent — while a local
// .env the platform never had stays behind.
func TestPackRewritesAPulledManifest(t *testing.T) {

	dir := t.TempDir()
	pulled := `{"version":2,"name":"orders","tag":"v0.1.0","configs":["octo.yaml","old.yaml"],
		"resources":[{"name":".env.prod","kind":"env"},{"name":"resources/env/prod.env","kind":"env"}]}`
	_, err := unpack(zipOf(t, map[string]string{
		manifestName:             pulled,
		"octo.yaml":              "flows: []",
		"old.yaml":               "flows: []",
		".env.prod":              "A=1",
		"resources/env/prod.env": "B=2",
	}), dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(dir, "old.yaml"))
	_ = os.WriteFile(filepath.Join(dir, "new.yaml"), []byte("flows: []"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "orders_test.yaml"), []byte("tests: []"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "welcome.tmpl"), []byte("hi"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, ".env.dev"), []byte("SECRET=1"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, ".octo"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".octo", "platform.json"), []byte("{}"), 0o644)

	files := packed(t, dir)
	for _, name := range []string{".env.dev", ".octo/platform.json", "old.yaml"} {
		if _, ok := files[name]; ok {
			t.Errorf("%s should not be pushed", name)
		}
	}
	if files[".env.prod"] != "A=1" {
		t.Errorf("the platform's own .env.prod should go back up, got %q", files[".env.prod"])
	}

	var m manifest
	if err := json.Unmarshal([]byte(files[manifestName]), &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != "orders" || m.Tag != "v0.1.0" || m.Version != 2 {
		t.Errorf("name, tag and version should carry over, got %+v", m)
	}
	if got := strings.Join(m.Configs, ","); got != "new.yaml,octo.yaml" {
		t.Errorf("configs are what is on disk now, got %s", got)
	}
	kinds := map[string]string{}
	for _, r := range m.Resources {
		kinds[r.Name] = r.Kind
	}
	want := map[string]string{
		".env.prod":              "env",
		"resources/env/prod.env": "env",
		"orders_test.yaml":       "template",
		"welcome.tmpl":           "template",
	}
	if len(kinds) != len(want) {
		t.Errorf("resources %v, want %v", kinds, want)
	}
	for name, kind := range want {
		if kinds[name] != kind {
			t.Errorf("%s: kind %q, want %q", name, kinds[name], kind)
		}
	}
}

// A folder that was never pulled has no manifest, and none is made up for it: the
// platform reads its layout instead.
func TestPackLeavesAFolderWithoutAManifestAlone(t *testing.T) {

	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "octo.yaml"), []byte("flows: []"), 0o644)
	if _, ok := packed(t, dir)[manifestName]; ok {
		t.Error("pack wrote a manifest for a folder that had none")
	}
}
