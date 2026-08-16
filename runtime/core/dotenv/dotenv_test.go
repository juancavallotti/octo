package dotenv

import (
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	data := []byte(`
# a comment
DB_HOST=localhost
export API_KEY = secret
QUOTED="quoted value"
SINGLE='single value'
EMPTY=
SPACED  =  trimmed

`)
	values, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	want := map[string]string{
		"DB_HOST": "localhost",
		"API_KEY": "secret",
		"QUOTED":  "quoted value",
		"SINGLE":  "single value",
		"EMPTY":   "",
		"SPACED":  "trimmed",
	}
	if len(values) != len(want) {
		t.Fatalf("parsed %d vars, want %d: %v", len(values), len(want), values)
	}
	for k, v := range want {
		if got := values[k]; got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestParseUnescapesDoubleQuotedValues(t *testing.T) {
	values, err := Parse([]byte(`QUOTE="say \"hi\""` + "\n" + `PATH_SEP="a\\b"` + "\n" + `LITERAL='say \"hi\"'`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := map[string]string{
		"QUOTE":    `say "hi"`,
		"PATH_SEP": `a\b`,
		// Single quotes are literal, so the backslashes survive.
		"LITERAL": `say \"hi\"`,
	}
	if !reflect.DeepEqual(values, want) {
		t.Errorf("Parse = %#v, want %#v", values, want)
	}
}

func TestFormat(t *testing.T) {
	got := string(Format(map[string]string{
		"ZULU":   "last",
		"ALPHA":  "first",
		"SPACED": "two words",
		"EMPTY":  "",
		"HASH":   "a#b",
		"QUOTE":  `say "hi"`,
	}))
	// Keys are sorted, and only the values that need it are quoted.
	want := "ALPHA=first\n" +
		"EMPTY=\"\"\n" +
		"HASH=\"a#b\"\n" +
		"QUOTE=\"say \\\"hi\\\"\"\n" +
		"SPACED=\"two words\"\n" +
		"ZULU=last\n"
	if got != want {
		t.Errorf("Format =\n%q\nwant\n%q", got, want)
	}
}

// TestFormatParseRoundTrip is the contract the pair exists for: whatever Format
// writes, Parse reads back unchanged.
func TestFormatParseRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]string
	}{
		{name: "plain", values: map[string]string{"A": "1", "B": "two"}},
		{name: "empty value", values: map[string]string{"A": ""}},
		{name: "whitespace", values: map[string]string{"A": "two words", "B": "\ttabbed"}},
		{name: "comment marker", values: map[string]string{"A": "a#b"}},
		{name: "double quotes", values: map[string]string{"A": `say "hi"`}},
		{name: "single quotes", values: map[string]string{"A": "it's"}},
		{name: "both quotes", values: map[string]string{"A": `it's "quoted"`}},
		{name: "backslashes", values: map[string]string{"A": `a\b\\c`}},
		{name: "wrapped in quotes", values: map[string]string{"A": `"wrapped"`}},
		{name: "equals sign", values: map[string]string{"A": "k=v"}},
		{name: "empty map", values: map[string]string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(Format(tc.values))
			if err != nil {
				t.Fatalf("Parse(Format(...)): %v", err)
			}
			if !reflect.DeepEqual(got, tc.values) {
				t.Errorf("round-trip = %#v, want %#v", got, tc.values)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr string
	}{
		{name: "missing equals", data: "NOT_AN_ASSIGNMENT", wantErr: "missing '='"},
		{name: "empty name", data: "=value", wantErr: "empty variable name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.data)); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}
