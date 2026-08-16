package expr

import (
	"reflect"
	"testing"
)

func TestToEnv(t *testing.T) {
	prog, err := CompileMessage(nil, `toEnv(body)`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	body := map[string]any{"DB_HOST": "localhost", "API_KEY": "s3cret", "DEBUG": true, "PORT": 8080}
	out, err := prog.EvalString(messageActivation(body))
	if err != nil {
		t.Fatalf("EvalString: %v", err)
	}
	// dotenv.Format sorts keys, so the rendering is deterministic.
	want := "API_KEY=s3cret\nDB_HOST=localhost\nDEBUG=true\nPORT=8080\n"
	if out != want {
		t.Errorf("toEnv = %q, want %q", out, want)
	}
}

func TestFromEnv(t *testing.T) {
	prog, err := CompileMessage(nil, `fromEnv(body)`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	const file = `
# the database
DB_HOST=localhost
export API_KEY = s3cret
GREETING="two words"
LITERAL='as written'
EMPTY=
`
	out, err := prog.Eval(messageActivation(file))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	want := map[string]any{
		"DB_HOST":  "localhost",
		"API_KEY":  "s3cret",
		"GREETING": "two words",
		"LITERAL":  "as written",
		"EMPTY":    "",
	}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("fromEnv = %#v, want %#v", out, want)
	}
}

// TestFromEnvValuesAreStrings pins the shape callers write expressions against:
// an env file has no types, so every value arrives as a string.
func TestFromEnvValuesAreStrings(t *testing.T) {
	prog, err := CompileMessage(nil, `fromEnv(body).PORT == "8080"`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	out, err := prog.Eval(messageActivation("PORT=8080\n"))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if out != true {
		t.Errorf("PORT == \"8080\" = %v, want true", out)
	}
}

func TestEnvRoundTrip(t *testing.T) {
	prog, err := CompileMessage(nil, `fromEnv(toEnv(body))`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	body := map[string]any{"A": "1", "GREETING": "two words", "QUOTED": `say "hi"`, "EMPTY": ""}
	out, err := prog.Eval(messageActivation(body))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	want := map[string]any{"A": "1", "GREETING": "two words", "QUOTED": `say "hi"`, "EMPTY": ""}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("round-trip = %#v, want %#v", out, want)
	}
}

func TestToEnvRejectsComposites(t *testing.T) {
	tests := []struct {
		name string
		body any
	}{
		{name: "nested object", body: map[string]any{"A": map[string]any{"k": "v"}}},
		{name: "list value", body: map[string]any{"A": []any{"x", "y"}}},
		{name: "not an object", body: []any{"a", "b"}},
	}
	prog, err := CompileMessage(nil, `toEnv(body)`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := prog.Eval(messageActivation(tc.body)); err == nil {
				t.Fatal("expected an evaluation error")
			}
		})
	}
}

func TestFromEnvErrors(t *testing.T) {
	tests := []struct {
		name string
		body any
	}{
		{name: "line without an assignment", body: "NOT_AN_ASSIGNMENT\n"},
		{name: "empty variable name", body: "=value\n"},
		{name: "argument is not a string", body: map[string]any{"A": 1}},
	}
	prog, err := CompileMessage(nil, `fromEnv(body)`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := prog.Eval(messageActivation(tc.body)); err == nil {
				t.Fatal("expected an evaluation error")
			}
		})
	}
}
