package schema

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
)

// applyDescriptions overlays field descriptions parsed from the settings struct's
// Go doc comments onto an already-built field list. srcDir is the source
// directory of the package the struct is declared in (captured at registration
// via runtime.Caller); t is the struct type the fields were reflected from.
//
// A field keeps a description it already has (e.g. from an octo desc= clause);
// otherwise it takes its struct field's doc comment. Nested object fields recurse
// into their own struct type. When the source cannot be read (e.g. running a
// prebuilt binary away from the repo) descriptions are simply left empty —
// generation still succeeds.
func applyDescriptions(fields []Field, srcDir string, t reflect.Type) {
	docs := loadPackageDocs(srcDir)
	if docs == nil {
		return
	}
	overlay(fields, t, docs)
}

// overlay walks the struct type in parallel with its Field specs, matching by
// json field name and filling empty descriptions from the doc map for that
// struct type. It recurses into nested object structs.
func overlay(fields []Field, t reflect.Type, docs map[string]map[string]string) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	byName := docs[t.Name()]
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if strings.TrimSpace(sf.Tag.Get("octo")) == "" {
			continue
		}
		name := jsonName(sf)
		if name == "-" {
			continue
		}
		idx := fieldIndex(fields, name)
		if idx < 0 {
			continue
		}
		if fields[idx].Description == "" {
			if doc := byName[name]; doc != "" {
				fields[idx].Description = doc
			}
		}
		if fields[idx].Type == "object" {
			overlay(fields[idx].Fields, sf.Type, docs)
		}
	}
}

func fieldIndex(fields []Field, name string) int {
	for i := range fields {
		if fields[i].Name == name {
			return i
		}
	}
	return -1
}

// docCache memoizes parsed package docs by source directory. Generation is a
// one-shot process, so the source is stable for the run.
var docCache sync.Map // map[string]map[string]map[string]string

// loadPackageDocs parses every non-test Go file in dir and returns, per struct
// type name, a map of json field name to cleaned doc comment. It returns nil
// when the directory is empty or cannot be parsed.
func loadPackageDocs(dir string) map[string]map[string]string {
	if dir == "" {
		return nil
	}
	if cached, ok := docCache.Load(dir); ok {
		return cached.(map[string]map[string]string)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		docCache.Store(dir, map[string]map[string]string(nil))
		return nil
	}
	fset := token.NewFileSet()
	out := make(map[string]map[string]string)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		// Parse each source file directly: parser.ParseDir is deprecated (it
		// ignores build tags), and build tags do not matter for doc extraction.
		file, perr := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ParseComments)
		if perr != nil {
			continue // skip an unparsable file; other files still contribute
		}
		collectStructDocs(file, out)
	}
	docCache.Store(dir, out)
	return out
}

// collectStructDocs records the field doc comments of every struct type in file.
func collectStructDocs(file *ast.File, out map[string]map[string]string) {
	ast.Inspect(file, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return true
		}
		byName := make(map[string]string)
		for _, astField := range st.Fields.List {
			doc := cleanDoc(astField.Doc.Text())
			if doc == "" {
				continue
			}
			name := astFieldJSONName(astField)
			if name == "" || name == "-" {
				continue
			}
			byName[name] = doc
		}
		if len(byName) > 0 {
			out[ts.Name.Name] = byName
		}
		return true
	})
}

// astFieldJSONName returns a struct field's json name from its tag, falling back
// to the Go field name. Embedded fields (no names) are skipped.
func astFieldJSONName(f *ast.Field) string {
	if len(f.Names) == 0 {
		return ""
	}
	goName := f.Names[0].Name
	if f.Tag == nil {
		return goName
	}
	tag := reflect.StructTag(strings.Trim(f.Tag.Value, "`"))
	jsonTag := tag.Get("json")
	if jsonTag == "" {
		return goName
	}
	name, _, _ := strings.Cut(jsonTag, ",")
	if name == "" {
		return goName
	}
	return name
}

// cleanDoc collapses a doc comment (which preserves source line breaks) into a
// single-line description: leading/trailing space trimmed, all internal runs of
// whitespace reduced to one space.
func cleanDoc(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
