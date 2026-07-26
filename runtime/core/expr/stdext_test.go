package expr

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// The libraries are registered as a message extension, so this table doubles as
// the statement of what an Octo expression may call. One case per library keeps a
// removed or renamed registration from passing silently; the wider vocabulary is
// cel-go's own to test.
func TestStandardExtensionLibraries(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want any
	}{
		{"strings upperAscii", `body.name.upperAscii()`, "ADA"},
		{"strings split", `body.csv.split(",")`, []any{"a", "b", "c"}},
		{"strings join", `body.csv.split(",").join(" | ")`, "a | b | c"},
		{"strings trim and replace", `"  a-b  ".trim().replace("-", "_")`, "a_b"},
		{"strings format", `"order %d: %s".format([7, body.name])`, "order 7: Ada"},
		{"lists distinct", `[1, 2, 2, 3].distinct()`, []any{1.0, 2.0, 3.0}},
		{"lists sortBy", `body.items.sortBy(i, i.qty).map(i, i.sku)`, []any{"b", "a"}},
		{"lists flatten", `[[1, 2], [3]].flatten()`, []any{1.0, 2.0, 3.0}},
		{"lists slice", `[1, 2, 3, 4].slice(1, 3)`, []any{2.0, 3.0}},
		{"lists range", `lists.range(3)`, []any{0.0, 1.0, 2.0}},
		{"encoders base64 encode", `base64.encode(bytes(body.name))`, "QWRh"},
		{"encoders base64 decode", `string(base64.decode("QWRh"))`, "Ada"},
		{"math greatest", `math.greatest(body.items.map(i, i.qty))`, 5.0},
		{"math round", `math.round(2.5)`, 3.0},
		{"comprehensions transformMap", `{"a": 1, "b": 2}.transformMap(k, v, k + string(v))`,
			map[string]any{"a": "a1", "b": "b2"}},
		{"comprehensions transformList", `body.items.transformList(i, v, string(i) + v.sku)`,
			[]any{"0a", "1b"}},
		{"comprehensions two-var exists", `{"a": 1}.exists(k, v, k == "a" && v == 1)`, true},
		{"sets contains", `sets.contains(["read", "write"], ["read"])`, true},
		{"sets intersects", `sets.intersects(["read"], ["write"])`, false},
		{"regex extract", `regex.extract(body.ref, "id:(\\d+)")`, "123"},
		{"regex extractAll", `regex.extractAll(body.ref, "\\d+")`, []any{"123"}},
		{"regex replace", `regex.replace(body.ref, "\\d+", "N")`, "id:N"},
	}

	body := map[string]any{
		"name": "Ada",
		"csv":  "a,b,c",
		"ref":  "id:123",
		"items": []any{
			map[string]any{"sku": "a", "qty": 5.0},
			map[string]any{"sku": "b", "qty": 1.0},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := CompileMessage(nil, tc.expr)
			if err != nil {
				t.Fatalf("CompileMessage(%q): %v", tc.expr, err)
			}
			got, err := prog.Eval(messageActivation(body))
			if err != nil {
				t.Fatalf("Eval(%q): %v", tc.expr, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("%s = %#v, want %#v", tc.expr, got, tc.want)
			}
		})
	}
}

// The libraries reach every scope through the same seam, so a source payload and
// a template span see them too — not just message expressions.
func TestStandardExtensionLibrariesInEveryScope(t *testing.T) {
	payload, err := CompileSourcePayload(nil, `settings.topic.upperAscii()`)
	if err != nil {
		t.Fatalf("CompileSourcePayload: %v", err)
	}
	got, err := payload.Eval(map[string]any{"now": time.Unix(0, 0), "settings": map[string]any{"topic": "orders"}})
	if err != nil {
		t.Fatalf("Eval payload: %v", err)
	}
	if got != "ORDERS" {
		t.Errorf("payload = %#v, want %q", got, "ORDERS")
	}

	loader := fakeLoader{"greet": []byte(`{{ body.name.upperAscii() }}`)}
	tpl, err := CompileMessage(loader, `templateResource("greet")`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	rendered, err := tpl.EvalString(messageActivation(map[string]any{"name": "Ada"}))
	if err != nil {
		t.Fatalf("EvalString: %v", err)
	}
	if rendered != "ADA" {
		t.Errorf("template = %q, want %q", rendered, "ADA")
	}
}

// regex.extract reports "no match" as optional.none(), which has no JSON
// counterpart. Eval resolves it to null so a miss is an absent value the flow can
// test, not an evaluation error that fails the message.
func TestOptionalResultBecomesNull(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want any
	}{
		{"absent", `regex.extract(body.ref, "user:(\\w+)")`, nil},
		{"present", `regex.extract(body.ref, "id:(\\d+)")`, "123"},
		{"orValue on a miss", `regex.extract(body.ref, "user:(\\w+)").orValue("anon")`, "anon"},
		{"tested with has", `has(body.missing)`, false},
		// Optionals nest, so unwrapping has to run to the bottom.
		{"nested absent", `optional.of(regex.extract(body.ref, "user:(\\w+)"))`, nil},
		{"nested present", `optional.of(regex.extract(body.ref, "id:(\\d+)"))`, "123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := CompileMessage(nil, tc.expr)
			if err != nil {
				t.Fatalf("CompileMessage(%q): %v", tc.expr, err)
			}
			got, err := prog.Eval(messageActivation(map[string]any{"ref": "id:123"}))
			if err != nil {
				t.Fatalf("Eval(%q): %v", tc.expr, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("%s = %#v, want %#v", tc.expr, got, tc.want)
			}
		})
	}
}

// Pinning is the point of the version constants: an unpinned library tracks
// whatever cel-go ships. This asserts the vocabulary the docs and the editor
// catalogue describe is the vocabulary that compiles — the version-gated entries
// are the ones a downgrade would silently drop.
func TestPinnedLibraryVersionsExposeTheDocumentedFunctions(t *testing.T) {
	// Each expression needs its library pinned at or above the version that
	// introduced it: reverse needs strings 3, sortBy needs lists 2, and the
	// bit operations need math 1.
	versionGated := []string{
		`"abc".reverse()`,
		`strings.quote("a\"b")`,
		`[3, 1].sortBy(i, i)`,
		`[1, 2, 3].last().orValue(0)`,
		`math.bitAnd(6, 3)`,
		`math.isFinite(1.0)`,
	}
	for _, expression := range versionGated {
		if _, err := CompileMessage(nil, expression); err != nil {
			t.Errorf("CompileMessage(%q): %v", expression, err)
		}
	}
}

// ext.Bindings is deliberately not registered (nor ext.Protos/ext.NativeTypes,
// which have no JSON-message equivalent to test against). A test pins the decision
// so re-adding one is a conscious edit rather than a side effect of touching
// standardExtensionOptions.
func TestBindingsStaysUnavailable(t *testing.T) {
	_, err := CompileMessage(nil, `cel.bind(x, 1, x + 1)`)
	if err == nil {
		t.Fatal("cel.bind compiled; the bindings library is deliberately not registered")
	}
	if !strings.Contains(err.Error(), "undeclared reference to 'cel'") {
		t.Errorf("error = %v, want an undeclared reference to 'cel'", err)
	}
}
