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
	got, err := Format(map[string]string{
		"ZULU":   "last",
		"ALPHA":  "first",
		"SPACED": "two words",
		"EMPTY":  "",
		"HASH":   "a#b",
		"QUOTE":  `say "hi"`,
	})
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	// Keys are sorted, and only the values that need it are quoted.
	want := "ALPHA=first\n" +
		"EMPTY=\"\"\n" +
		"HASH=\"a#b\"\n" +
		"QUOTE=\"say \\\"hi\\\"\"\n" +
		"SPACED=\"two words\"\n" +
		"ZULU=last\n"
	if string(got) != want {
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
		{name: "newline", values: map[string]string{"A": "line one\nline two"}},
		{name: "carriage return", values: map[string]string{"A": "a\r\nb"}},
		{name: "multi-line certificate", values: map[string]string{
			"KEY": "-----BEGIN KEY-----\nabc\ndef\n-----END KEY-----\n",
		}},
		{name: "escape sequences as text", values: map[string]string{"A": `not \n a newline`}},
		{name: "leading and trailing space", values: map[string]string{"A": "  padded  "}},
		{name: "looks like a comment", values: map[string]string{"A": "# not a comment"}},
		{name: "looks like an export", values: map[string]string{"A": "export B=2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := Format(tc.values)
			if err != nil {
				t.Fatalf("Format: %v", err)
			}
			got, err := Parse(data)
			if err != nil {
				t.Fatalf("Parse(Format(...)): %v", err)
			}
			if !reflect.DeepEqual(got, tc.values) {
				t.Errorf("round-trip = %#v, want %#v", got, tc.values)
			}
		})
	}
}

// TestFormatValueCannotInjectAnAssignment is the security half of the round-trip
// contract. Parse reads one physical line per entry, so a value carrying a
// newline must not be able to smuggle a second variable into the file — the
// classic injection when untrusted data reaches a config writer.
func TestFormatValueCannotInjectAnAssignment(t *testing.T) {
	const hostile = "x\nINJECTED=1\n#"
	data, err := Format(map[string]string{"A": hostile})
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if lines := strings.Count(string(data), "\n"); lines != 1 {
		t.Errorf("Format wrote %d lines, want 1: %q", lines, data)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, ok := got["INJECTED"]; ok {
		t.Errorf("a value introduced a second variable: %#v", got)
	}
	if got["A"] != hostile {
		t.Errorf("A = %q, want %q", got["A"], hostile)
	}
}

// TestFormatRejectsUnwritableKeys covers the half that cannot be escaped: a name
// has no quoted form, so a name Parse would read as something else is refused
// rather than written.
func TestFormatRejectsUnwritableKeys(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr string
	}{
		{name: "empty", key: "", wantErr: "is empty"},
		{name: "equals sign", key: "A=B", wantErr: `contains '='`},
		{name: "space", key: "A B", wantErr: `contains ' '`},
		{name: "trailing export prefix", key: "export A", wantErr: `contains ' '`},
		{name: "newline", key: "A\nB", wantErr: `contains '\n'`},
		{name: "comment marker", key: "#A", wantErr: `contains '#'`},
		{name: "quote", key: `A"B`, wantErr: `contains '"'`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Format(map[string]string{tc.key: "v"})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
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
